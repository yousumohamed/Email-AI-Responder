package email

import (
	"strings"
	"testing"

	"ai-email-responder/config"
)

func TestExtractEmailAddress(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"John Doe <john@example.com>", "john@example.com"},
		{"<jane.smith@sub.domain.co>", "jane.smith@sub.domain.co"},
		{"simple@example.com", "simple@example.com"},
		{"Capitalized@EXAMPLE.COM", "capitalized@example.com"},
		{"\"Quotes, Name\" <user@domain.org>", "user@domain.org"},
		{"", ""},
	}

	for _, tt := range tests {
		got := ExtractEmailAddress(tt.input)
		if got != tt.expected {
			t.Errorf("ExtractEmailAddress(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFormatReplySubject(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Website Inquiry", "Re: Website Inquiry"},
		{"Re: Website Inquiry", "Re: Website Inquiry"},
		{"RE: Website Inquiry", "Re: Website Inquiry"},
		{"Re: Re: Website Inquiry", "Re: Website Inquiry"},
		{"re: RE:  Another Question", "Re: Another Question"},
		{"", "Re: (No Subject)"},
	}

	for _, tt := range tests {
		got := FormatReplySubject(tt.input)
		if got != tt.expected {
			t.Errorf("FormatReplySubject(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDetermineRecipient(t *testing.T) {
	msgWithReplyTo := &Message{
		From:      "sender@example.com",
		FromEmail: "sender@example.com",
		ReplyTo:   "Direct Replies <replies@example.com>",
	}
	if got := DetermineRecipient(msgWithReplyTo); got != "replies@example.com" {
		t.Errorf("expected replies@example.com, got %s", got)
	}

	msgWithoutReplyTo := &Message{
		From:      "Customer Name <customer@domain.com>",
		FromEmail: "customer@domain.com",
	}
	if got := DetermineRecipient(msgWithoutReplyTo); got != "customer@domain.com" {
		t.Errorf("expected customer@domain.com, got %s", got)
	}
}

func TestHTMLToPlainText(t *testing.T) {
	htmlInput := `
	<!DOCTYPE html>
	<html>
	<head><style>body { color: red; }</style><script>alert('hack');</script></head>
	<body>
		<p>Hello world!</p>
		<div>This is a paragraph with <a href="https://example.com">a link</a> &amp; special characters.</div>
		<br/>
		<p>Thank you.</p>
	</body>
	</html>
	`

	text := HTMLToPlainText(htmlInput)
	if strings.Contains(text, "alert('hack')") {
		t.Errorf("scripts should be stripped from plain text output")
	}
	if strings.Contains(text, "color: red") {
		t.Errorf("styles should be stripped from plain text output")
	}
	if !strings.Contains(text, "Hello world!") {
		t.Errorf("missing expected text content 'Hello world!'")
	}
	if !strings.Contains(text, "& special characters.") {
		t.Errorf("expected unescaped HTML entities, got: %s", text)
	}
}

func TestSanitizeAndTruncateBody(t *testing.T) {
	longBody := strings.Repeat("A", 200)
	truncated, wasTruncated := SanitizeAndTruncateBody(longBody, false, 50)
	if !wasTruncated {
		t.Errorf("expected body to be truncated")
	}
	if !strings.Contains(truncated, "... [Email truncated due to length limits]") {
		t.Errorf("expected truncation indicator")
	}
}

func TestApplySignatureAndCleanPlaceholders(t *testing.T) {
	cfg := &config.Config{
		ReplyName:    "Som DVPS",
		ReplyTitle:   "Support Manager",
		CompanyName:  "Som DVPS Solutions",
		ReplyContact: "somdvps@gmail.com",
	}

	rawReply := `Hello Jose,

Thank you for your question.

Best regards,

[Your Name]
[Your Position]
[Your Company Name]
[Your Contact Information, if applicable]`

	cleaned := ApplySignatureAndCleanPlaceholders(rawReply, cfg)

	if strings.Contains(cleaned, "[Your Name]") {
		t.Errorf("failed to replace [Your Name]: %s", cleaned)
	}
	if !strings.Contains(cleaned, "Som DVPS") {
		t.Errorf("expected signature to contain 'Som DVPS', got: %s", cleaned)
	}
	if !strings.Contains(cleaned, "Support Manager") {
		t.Errorf("expected signature to contain 'Support Manager', got: %s", cleaned)
	}
	if !strings.Contains(cleaned, "Som DVPS Solutions") {
		t.Errorf("expected signature to contain 'Som DVPS Solutions', got: %s", cleaned)
	}
	if !strings.Contains(cleaned, "somdvps@gmail.com") {
		t.Errorf("expected signature to contain contact, got: %s", cleaned)
	}
}
