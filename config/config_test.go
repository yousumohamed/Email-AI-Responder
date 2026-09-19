package config

import (
	"os"
	"strings"
	"testing"
)

func TestConfigValidation_MissingVars(t *testing.T) {
	os.Clearenv()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when required environment variables are missing, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "Missing required environment variable:") {
		t.Fatalf("unexpected error format: %s", errMsg)
	}
	for _, expectedVar := range []string{"GMAIL_EMAIL", "GMAIL_APP_PASSWORD", "OPENROUTER_API_KEY", "OPENROUTER_MODEL"} {
		if !strings.Contains(errMsg, expectedVar) {
			t.Errorf("expected error message to mention %s, got: %s", expectedVar, errMsg)
		}
	}
}

func TestConfigValidation_SuccessAndRedaction(t *testing.T) {
	os.Clearenv()
	t.Setenv("GMAIL_EMAIL", "test@gmail.com")
	t.Setenv("GMAIL_APP_PASSWORD", "secretapppassword123")
	t.Setenv("OPENROUTER_API_KEY", "sk-secret-openrouter-key")
	t.Setenv("OPENROUTER_MODEL", "google/gemini-2.5-flash")
	t.Setenv("AUTO_REPLY_ENABLED", "true")
	t.Setenv("REQUIRE_APPROVAL", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected valid config load, got: %v", err)
	}

	if !cfg.CanAutoSend() {
		t.Errorf("expected CanAutoSend() to be true when AUTO_REPLY_ENABLED=true and REQUIRE_APPROVAL=false")
	}

	summary := cfg.RedactedSummary()
	if strings.Contains(summary, "secretapppassword123") {
		t.Errorf("summary leaked Gmail App Password: %s", summary)
	}
	if strings.Contains(summary, "sk-secret-openrouter-key") {
		t.Errorf("summary leaked OpenRouter API key: %s", summary)
	}
}
