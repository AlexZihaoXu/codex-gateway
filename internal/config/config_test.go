package config

import "testing"

func TestLoadUsesSafeDefaults(t *testing.T) {
	configuration, err := Load(testEnvironment(nil))
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	if configuration.Address != "127.0.0.1:8080" {
		t.Fatalf("expected loopback address, got %q", configuration.Address)
	}
	if configuration.DataDirectory != "./data" {
		t.Fatalf("expected local data directory, got %q", configuration.DataDirectory)
	}
	if configuration.LogLevel != "info" {
		t.Fatalf("expected info log level, got %q", configuration.LogLevel)
	}
}

func TestLoadAcceptsEnvironmentOverrides(t *testing.T) {
	values := map[string]string{
		"HOST":      "0.0.0.0",
		"PORT":      "11435",
		"DATA_DIR":  "/data",
		"LOG_LEVEL": "debug",
	}
	configuration, err := Load(testEnvironment(values))
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	if configuration.Address != "0.0.0.0:11435" {
		t.Fatalf("unexpected address: %q", configuration.Address)
	}
	if configuration.DataDirectory != "/data" {
		t.Fatalf("unexpected data directory: %q", configuration.DataDirectory)
	}
	if configuration.LogLevel != "debug" {
		t.Fatalf("unexpected log level: %q", configuration.LogLevel)
	}
}

func TestLoadRejectsInvalidPort(t *testing.T) {
	_, err := Load(testEnvironment(map[string]string{
		"PORT": "not-a-port",
	}))
	if err == nil {
		t.Fatal("expected invalid port to fail")
	}
}

func TestLoadRejectsUnknownLogLevel(t *testing.T) {
	_, err := Load(testEnvironment(map[string]string{"LOG_LEVEL": "verbose"}))
	if err == nil {
		t.Fatal("expected unknown log level to fail")
	}
}

func TestLoadRequiresCompleteFirebaseConfiguration(t *testing.T) {
	for _, missing := range []string{
		"FIREBASE_API_KEY",
		"FIREBASE_AUTH_DOMAIN",
		"FIREBASE_PROJECT_ID",
		"FIREBASE_APP_ID",
		"FIREBASE_ADMIN_EMAIL",
	} {
		t.Run(missing, func(t *testing.T) {
			values := firebaseTestValues()
			delete(values, missing)
			if _, err := Load(func(key string) string { return values[key] }); err == nil {
				t.Fatalf("expected missing %s to be rejected", missing)
			}
		})
	}
}

func TestLoadNormalizesFirebaseAccessList(t *testing.T) {
	configuration, err := Load(testEnvironment(map[string]string{
		"FIREBASE_ADMIN_EMAIL":    " Administrator@Example.Test ",
		"FIREBASE_ALLOWED_EMAILS": " work@example.com,WORK@example.com, teammate@example.com ",
	}))
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	if configuration.Firebase.AdministratorEmail != "administrator@example.test" {
		t.Fatalf("unexpected administrator email: %q", configuration.Firebase.AdministratorEmail)
	}
	if len(configuration.Firebase.AllowedEmails) != 2 || configuration.Firebase.AllowedEmails[0] != "work@example.com" || configuration.Firebase.AllowedEmails[1] != "teammate@example.com" {
		t.Fatalf("unexpected allowed emails: %#v", configuration.Firebase.AllowedEmails)
	}
}

func firebaseTestValues() map[string]string {
	return map[string]string{
		"FIREBASE_API_KEY":     "test-api-key",
		"FIREBASE_AUTH_DOMAIN": "test.firebaseapp.com",
		"FIREBASE_PROJECT_ID":  "test-project",
		"FIREBASE_APP_ID":      "test-app-id",
		"FIREBASE_ADMIN_EMAIL": "admin@example.com",
	}
}

func testEnvironment(overrides map[string]string) LookupEnv {
	values := firebaseTestValues()
	for key, value := range overrides {
		values[key] = value
	}
	return func(key string) string { return values[key] }
}
