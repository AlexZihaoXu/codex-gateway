package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	firebaseCertificateURL    = "https://www.googleapis.com/robot/v1/metadata/x509/securetoken@system.gserviceaccount.com"
	maximumFirebaseTokenBytes = 16 * 1024
	maximumCertificateBytes   = 1024 * 1024
	defaultCertificateMaxAge  = time.Hour
	firebaseClockSkew         = time.Minute
)

var (
	ErrInvalidFirebaseToken    = errors.New("invalid Firebase ID token")
	ErrFirebaseKeysUnavailable = errors.New("Firebase signing keys are unavailable")
)

type IdentityVerifier interface {
	Verify(context.Context, string) (Identity, error)
}

type FirebaseVerifierOptions struct {
	ProjectID string
	Clock     func() time.Time
	Client    *http.Client
}

type FirebaseVerifier struct {
	projectID string
	clock     func() time.Time
	client    *http.Client

	mutex          sync.Mutex
	keys           map[string]*rsa.PublicKey
	keysExpiration time.Time
}

type firebaseTokenHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
}

type firebaseTokenClaims struct {
	Audience           string `json:"aud"`
	Issuer             string `json:"iss"`
	Subject            string `json:"sub"`
	Expiration         int64  `json:"exp"`
	IssuedAt           int64  `json:"iat"`
	AuthenticationTime int64  `json:"auth_time"`
	Email              string `json:"email"`
	EmailVerified      bool   `json:"email_verified"`
	Firebase           struct {
		SignInProvider string `json:"sign_in_provider"`
	} `json:"firebase"`
}

func NewFirebaseVerifier(options FirebaseVerifierOptions) (*FirebaseVerifier, error) {
	projectID := strings.TrimSpace(options.ProjectID)
	if projectID == "" {
		return nil, errors.New("Firebase project ID is required")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 10 * time.Second}
	}
	return &FirebaseVerifier{projectID: projectID, clock: options.Clock, client: options.Client}, nil
}

func (verifier *FirebaseVerifier) Verify(ctx context.Context, token string) (Identity, error) {
	if len(token) == 0 || len(token) > maximumFirebaseTokenBytes {
		return Identity{}, invalidFirebaseToken("token length is invalid")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, invalidFirebaseToken("token must contain three segments")
	}

	var header firebaseTokenHeader
	if err := decodeFirebaseSegment(parts[0], &header); err != nil {
		return Identity{}, invalidFirebaseToken("header is not valid JSON")
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		return Identity{}, invalidFirebaseToken("signing header is not supported")
	}
	var claims firebaseTokenClaims
	if err := decodeFirebaseSegment(parts[1], &claims); err != nil {
		return Identity{}, invalidFirebaseToken("claims are not valid JSON")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, invalidFirebaseToken("signature is not base64url")
	}

	key, err := verifier.signingKey(ctx, header.KeyID)
	if err != nil {
		return Identity{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return Identity{}, invalidFirebaseToken("signature does not verify")
	}
	if err := verifier.validateClaims(claims); err != nil {
		return Identity{}, err
	}
	return Identity{UID: claims.Subject, Email: strings.ToLower(strings.TrimSpace(claims.Email))}, nil
}

func (verifier *FirebaseVerifier) validateClaims(claims firebaseTokenClaims) error {
	now := verifier.clock().UTC()
	if claims.Audience != verifier.projectID {
		return invalidFirebaseToken("audience is not this Firebase project")
	}
	if claims.Issuer != "https://securetoken.google.com/"+verifier.projectID {
		return invalidFirebaseToken("issuer is not this Firebase project")
	}
	if claims.Subject == "" || len(claims.Subject) > 128 {
		return invalidFirebaseToken("subject is invalid")
	}
	if claims.Expiration == 0 || time.Unix(claims.Expiration, 0).Before(now.Add(-firebaseClockSkew)) {
		return invalidFirebaseToken("token has expired")
	}
	if claims.IssuedAt == 0 || time.Unix(claims.IssuedAt, 0).After(now.Add(firebaseClockSkew)) {
		return invalidFirebaseToken("token was issued in the future")
	}
	if claims.AuthenticationTime == 0 || time.Unix(claims.AuthenticationTime, 0).After(now.Add(firebaseClockSkew)) {
		return invalidFirebaseToken("authentication happened in the future")
	}
	if strings.TrimSpace(claims.Email) == "" || !claims.EmailVerified {
		return invalidFirebaseToken("verified email is required")
	}
	if claims.Firebase.SignInProvider != "google.com" {
		return invalidFirebaseToken("Google is the only accepted sign-in provider")
	}
	return nil
}

func (verifier *FirebaseVerifier) signingKey(ctx context.Context, keyID string) (*rsa.PublicKey, error) {
	keys, err := verifier.signingKeys(ctx, false)
	if err != nil {
		return nil, err
	}
	if key := keys[keyID]; key != nil {
		return key, nil
	}
	keys, err = verifier.signingKeys(ctx, true)
	if err != nil {
		return nil, err
	}
	if key := keys[keyID]; key != nil {
		return key, nil
	}
	return nil, invalidFirebaseToken("signing key is unknown")
}

func (verifier *FirebaseVerifier) signingKeys(ctx context.Context, force bool) (map[string]*rsa.PublicKey, error) {
	verifier.mutex.Lock()
	defer verifier.mutex.Unlock()
	now := verifier.clock().UTC()
	if !force && len(verifier.keys) > 0 && now.Before(verifier.keysExpiration) {
		return verifier.keys, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, firebaseCertificateURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: create request", ErrFirebaseKeysUnavailable)
	}
	response, err := verifier.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFirebaseKeysUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrFirebaseKeysUnavailable, response.StatusCode)
	}

	limited := io.LimitReader(response.Body, maximumCertificateBytes+1)
	encoded, err := io.ReadAll(limited)
	if err != nil || len(encoded) > maximumCertificateBytes {
		return nil, fmt.Errorf("%w: invalid response body", ErrFirebaseKeysUnavailable)
	}
	var certificates map[string]string
	if err := json.Unmarshal(encoded, &certificates); err != nil {
		return nil, fmt.Errorf("%w: invalid certificate document", ErrFirebaseKeysUnavailable)
	}
	keys := make(map[string]*rsa.PublicKey, len(certificates))
	for keyID, encodedCertificate := range certificates {
		block, _ := pem.Decode([]byte(encodedCertificate))
		if block == nil || block.Type != "CERTIFICATE" {
			continue
		}
		certificate, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			continue
		}
		publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
		if ok {
			keys[keyID] = publicKey
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: no usable certificates", ErrFirebaseKeysUnavailable)
	}
	verifier.keys = keys
	verifier.keysExpiration = now.Add(certificateMaxAge(response.Header.Get("Cache-Control")))
	return keys, nil
}

func decodeFirebaseSegment(segment string, destination any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, destination)
}

func invalidFirebaseToken(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidFirebaseToken, reason)
}

func certificateMaxAge(cacheControl string) time.Duration {
	for _, directive := range strings.Split(cacheControl, ",") {
		name, value, found := strings.Cut(strings.TrimSpace(directive), "=")
		if !found || strings.ToLower(name) != "max-age" {
			continue
		}
		seconds, err := strconv.ParseInt(strings.Trim(value, `"`), 10, 64)
		if err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultCertificateMaxAge
}
