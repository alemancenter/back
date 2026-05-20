package config

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Set environment variables to test overriding defaults
	os.Setenv("APP_NAME", "Test API")
	os.Setenv("APP_PORT", "9090")
	os.Setenv("APP_DEBUG", "true")
	os.Setenv("JWT_SECRET", "supersecret1234567890123456789012")
	os.Setenv("DB_HOST_JO", "localhost")
	os.Setenv("DB_NAME_JO", "jo")
	os.Setenv("DB_USER_JO", "root")
	os.Setenv("APP_URL", "http://localhost:8080")
	os.Setenv("FRONTEND_URL", "http://localhost:3000")

	// Ensure once is reset for testing if needed (though usually tests run in isolated processes)
	// Alternatively, just call Load() and check if it picks up env vars
	c := Load()

	if c == nil {
		t.Fatal("Load() returned nil")
	}

	if c.App.Name != "Test API" {
		t.Errorf("Expected APP_NAME 'Test API', got '%s'", c.App.Name)
	}

	if c.App.Port != 9090 {
		t.Errorf("Expected APP_PORT 9090, got '%d'", c.App.Port)
	}

	if !c.App.Debug {
		t.Error("Expected APP_DEBUG to be true")
	}

	if c.JWT.Secret != "supersecret1234567890123456789012" {
		t.Errorf("Expected JWT_SECRET 'supersecret1234567890123456789012', got '%s'", c.JWT.Secret)
	}

	// Clean up
	os.Unsetenv("APP_NAME")
	os.Unsetenv("APP_PORT")
	os.Unsetenv("APP_DEBUG")
	os.Unsetenv("JWT_SECRET")
}

func TestGet(t *testing.T) {
	c := Get()
	if c == nil {
		t.Fatal("Get() returned nil")
	}
}
