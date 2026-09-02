package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strconv"
	"strings"
)

const (
	defaultDataDirectory = "./data"
	defaultHost          = "127.0.0.1"
	defaultLogLevel      = "info"
	defaultPort          = 8080
)

type Config struct {
	Address       string
	DataDirectory string
	Firebase      FirebaseConfig
	LogLevel      string
}

type FirebaseConfig struct {
	APIKey             string
	AuthDomain         string
	ProjectID          string
	AppID              string
	AdministratorEmail string
	AllowedEmails      []string
}

type LookupEnv func(string) string

func Load(lookup LookupEnv) (Config, error) {
	firebase, err := loadFirebase(lookup)
	if err != nil {
		return Config{}, err
	}
	host := valueOrDefault(lookup("HOST"), defaultHost)
	dataDirectory := valueOrDefault(lookup("DATA_DIR"), defaultDataDirectory)
	logLevel := valueOrDefault(lookup("LOG_LEVEL"), defaultLogLevel)
	if !isLogLevel(logLevel) {
		return Config{}, fmt.Errorf("unsupported LOG_LEVEL %q", logLevel)
	}

	port := defaultPort
	if value := lookup("PORT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 65535 {
			return Config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
		}
		port = parsed
	}

	return Config{
		Address:       net.JoinHostPort(host, strconv.Itoa(port)),
		DataDirectory: dataDirectory,
		Firebase:      firebase,
		LogLevel:      logLevel,
	}, nil
}

func loadFirebase(lookup LookupEnv) (FirebaseConfig, error) {
	required := []string{
		"FIREBASE_API_KEY",
		"FIREBASE_AUTH_DOMAIN",
		"FIREBASE_PROJECT_ID",
		"FIREBASE_APP_ID",
		"FIREBASE_ADMIN_EMAIL",
	}
	values := make(map[string]string, len(required))
	for _, key := range required {
		value := strings.TrimSpace(lookup(key))
		if value == "" {
			return FirebaseConfig{}, fmt.Errorf("%s is required", key)
		}
		values[key] = value
	}
	administratorEmail, err := normalizedEmail(values["FIREBASE_ADMIN_EMAIL"])
	if err != nil {
		return FirebaseConfig{}, fmt.Errorf("FIREBASE_ADMIN_EMAIL must be a valid email address")
	}
	allowedEmails := make([]string, 0)
	seen := map[string]struct{}{administratorEmail: {}}
	for _, candidate := range strings.Split(lookup("FIREBASE_ALLOWED_EMAILS"), ",") {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		email, validationErr := normalizedEmail(candidate)
		if validationErr != nil {
			return FirebaseConfig{}, fmt.Errorf("FIREBASE_ALLOWED_EMAILS contains an invalid email address")
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		allowedEmails = append(allowedEmails, email)
	}
	return FirebaseConfig{
		APIKey:             values["FIREBASE_API_KEY"],
		AuthDomain:         values["FIREBASE_AUTH_DOMAIN"],
		ProjectID:          values["FIREBASE_PROJECT_ID"],
		AppID:              values["FIREBASE_APP_ID"],
		AdministratorEmail: administratorEmail,
		AllowedEmails:      allowedEmails,
	}, nil
}

func normalizedEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || !strings.Contains(normalized, "@") {
		return "", errors.New("invalid email address")
	}
	return normalized, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func isLogLevel(value string) bool {
	switch value {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}
