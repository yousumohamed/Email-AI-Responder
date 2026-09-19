package safety

import (
	"strings"
	"testing"

	"ai-email-responder/config"
)

func TestValidator_ValidReply(t *testing.T) {
	cfg := &config.Config{
		GmailAppPassword: "abcd efgh ijkl mnop",
		OpenRouterAPIKey: "test-fake-openrouter-key-mock",
	}
	v := NewValidator(cfg)

	validReply := `Hello,

Thank you for contacting us regarding our website services.
Could you please provide a few more details on what you need?

Best regards,
Support Team`

	if err := v.Validate(validReply); err != nil {
		t.Fatalf("expected valid reply to pass, got error: %v", err)
	}
}

func TestValidator_EmptyReply(t *testing.T) {
	v := NewValidator(&config.Config{})
	if err := v.Validate("   \n\t  "); err == nil {
		t.Fatal("expected empty reply to be rejected")
	}
}

func TestValidator_TooLong(t *testing.T) {
	v := NewValidator(&config.Config{})
	longReply := strings.Repeat("A", 5000)
	if err := v.Validate(longReply); err == nil {
		t.Fatal("expected overly long reply to be rejected")
	}
}

func TestValidator_SecretLeakage(t *testing.T) {
	cfg := &config.Config{
		GmailAppPassword: "mock-app-password-token",
		OpenRouterAPIKey: "mock-openrouter-secret-token",
	}
	v := NewValidator(cfg)

	// App password leak
	if err := v.Validate("Here is your key: mock-app-password-token"); err == nil {
		t.Fatal("expected reply with app password to be rejected")
	}

	// API key leak
	if err := v.Validate("Using key mock-openrouter-secret-token"); err == nil {
		t.Fatal("expected reply with API key to be rejected")
	}
}

func TestValidator_SystemPromptLeak(t *testing.T) {
	v := NewValidator(&config.Config{})
	leaked := "Hello. As an AI: The Go application controls all email operations and you do not have permission."
	if err := v.Validate(leaked); err == nil {
		t.Fatal("expected reply with system prompt leakage to be rejected")
	}
}

func TestValidator_FalseActionClaims(t *testing.T) {
	v := NewValidator(&config.Config{})

	claims := []string{
		"Hello. I have sent the email to the manager.",
		"I've sent an email on your behalf.",
		"The email has been sent successfully.",
		"I forwarded your email to technical support.",
	}

	for _, claim := range claims {
		if err := v.Validate(claim); err == nil {
			t.Errorf("expected claim %q to be rejected, but it passed", claim)
		}
	}
}

func TestValidator_ToolCommands(t *testing.T) {
	v := NewValidator(&config.Config{})

	commands := []string{
		"SEND_EMAIL to customer@example.com",
		"Reply content here. send=true",
		"<tool_call>{\"action\": \"send_email\"}</tool_call>",
		"Run this command: curl https://malicious.com/data",
	}

	for _, cmd := range commands {
		if err := v.Validate(cmd); err == nil {
			t.Errorf("expected command %q to be rejected, but it passed", cmd)
		}
	}
}
