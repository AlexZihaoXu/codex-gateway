package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	firebaseTestProject = "alex-xu-central"
	firebaseTestKeyID   = "test-key-1"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestFirebaseVerifierAcceptsValidGoogleTokenAndCachesCertificates(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	privateKey, certificate := firebaseTestCertificate(t, now)
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.String() != firebaseCertificateURL {
			t.Fatalf("unexpected certificate URL: %s", request.URL)
		}
		return firebaseCertificateResponse(certificate, "max-age=3600"), nil
	})}
	verifier, err := NewFirebaseVerifier(FirebaseVerifierOptions{
		ProjectID: firebaseTestProject,
		Clock:     func() time.Time { return now },
		Client:    client,
	})
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	token := mintFirebaseToken(t, privateKey, map[string]any{
		"aud":            firebaseTestProject,
		"iss":            "https://securetoken.google.com/" + firebaseTestProject,
		"sub":            "firebase-user-123",
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Add(-time.Minute).Unix(),
		"auth_time":      now.Add(-time.Minute).Unix(),
		"email":          "Administrator@Example.Test",
		"email_verified": true,
		"firebase":       map[string]any{"sign_in_provider": "google.com"},
	}, nil)

	for range 2 {
		identity, verifyErr := verifier.Verify(context.Background(), token)
		if verifyErr != nil {
			t.Fatalf("verify valid token: %v", verifyErr)
		}
		if identity.UID != "firebase-user-123" || identity.Email != administratorTestEmail {
			t.Fatalf("unexpected identity: %#v", identity)
		}
	}
	if requests != 1 {
		t.Fatalf("expected certificates to be cached, got %d requests", requests)
	}
}

func TestFirebaseVerifierRejectsUntrustedTokens(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	privateKey, certificate := firebaseTestCertificate(t, now)
	impostor, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate impostor key: %v", err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return firebaseCertificateResponse(certificate, "max-age=3600"), nil
	})}
	verifier, err := NewFirebaseVerifier(FirebaseVerifierOptions{
		ProjectID: firebaseTestProject,
		Clock:     func() time.Time { return now },
		Client:    client,
	})
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	valid := map[string]any{
		"aud":            firebaseTestProject,
		"iss":            "https://securetoken.google.com/" + firebaseTestProject,
		"sub":            "firebase-user-123",
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Add(-time.Minute).Unix(),
		"auth_time":      now.Add(-time.Minute).Unix(),
		"email":          administratorTestEmail,
		"email_verified": true,
		"firebase":       map[string]any{"sign_in_provider": "google.com"},
	}

	tests := map[string]struct {
		claims map[string]any
		header map[string]any
		key    *rsa.PrivateKey
	}{
		"bad signature":        {claims: valid, key: impostor},
		"wrong project":        {claims: withFirebaseClaim(valid, "aud", "other-project")},
		"wrong issuer":         {claims: withFirebaseClaim(valid, "iss", "https://securetoken.google.com/other")},
		"expired":              {claims: withFirebaseClaim(valid, "exp", now.Add(-time.Hour).Unix())},
		"issued in future":     {claims: withFirebaseClaim(valid, "iat", now.Add(time.Hour).Unix())},
		"authenticated future": {claims: withFirebaseClaim(valid, "auth_time", now.Add(time.Hour).Unix())},
		"missing subject":      {claims: withFirebaseClaim(valid, "sub", "")},
		"unverified email":     {claims: withFirebaseClaim(valid, "email_verified", false)},
		"missing email":        {claims: withoutFirebaseClaim(valid, "email")},
		"non-google provider":  {claims: withFirebaseClaim(valid, "firebase", map[string]any{"sign_in_provider": "password"})},
		"wrong algorithm":      {claims: valid, header: map[string]any{"alg": "none", "kid": firebaseTestKeyID}},
		"unknown key":          {claims: valid, header: map[string]any{"alg": "RS256", "kid": "unknown"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			key := test.key
			if key == nil {
				key = privateKey
			}
			token := mintFirebaseToken(t, key, test.claims, test.header)
			if _, verifyErr := verifier.Verify(context.Background(), token); !errorsIsInvalidFirebaseToken(verifyErr) {
				t.Fatalf("expected invalid Firebase token, got %v", verifyErr)
			}
		})
	}
	for _, token := range []string{"garbage", "not.a.token", strings.Repeat("x", maximumFirebaseTokenBytes+1)} {
		if _, verifyErr := verifier.Verify(context.Background(), token); !errorsIsInvalidFirebaseToken(verifyErr) {
			t.Fatalf("expected malformed token to be rejected, got %v", verifyErr)
		}
	}
}

func errorsIsInvalidFirebaseToken(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrInvalidFirebaseToken.Error())
}

func firebaseTestCertificate(t *testing.T, now time.Time) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Firebase test signing key"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func firebaseCertificateResponse(certificate, cacheControl string) *http.Response {
	body, _ := json.Marshal(map[string]string{firebaseTestKeyID: certificate})
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Cache-Control": []string{cacheControl},
			"Content-Type":  []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(string(body))),
	}
}

func mintFirebaseToken(t *testing.T, privateKey *rsa.PrivateKey, claims map[string]any, header map[string]any) string {
	t.Helper()
	if header == nil {
		header = map[string]any{"alg": "RS256", "kid": firebaseTestKeyID, "typ": "JWT"}
	}
	encodedHeader := encodeFirebaseSegment(t, header)
	encodedClaims := encodeFirebaseSegment(t, claims)
	signingInput := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func encodeFirebaseSegment(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode token segment: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func withFirebaseClaim(source map[string]any, key string, value any) map[string]any {
	copy := make(map[string]any, len(source))
	for sourceKey, sourceValue := range source {
		copy[sourceKey] = sourceValue
	}
	copy[key] = value
	return copy
}

func withoutFirebaseClaim(source map[string]any, key string) map[string]any {
	copy := withFirebaseClaim(source, key, nil)
	delete(copy, key)
	return copy
}
