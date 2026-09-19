package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration values for the application.
type Config struct {
	// Application
	AppName             string
	AppEnv              string
	LogLevel            string
	PollIntervalSeconds int

	// Gmail Account
	GmailEmail       string
	GmailAppPassword string

	// IMAP
	IMAPHost    string
	IMAPPort    int
	IMAPMailbox string

	// SMTP
	SMTPHost string
	SMTPPort int

	// OpenRouter
	OpenRouterAPIKey  string
	OpenRouterModel   string
	OpenRouterBaseURL string

	// AI Configuration
	AISystemInstructionFile string
	AIUserInfoFile          string
	AITemperature           float64
	AIMaxTokens             int

	// Auto Reply
	AutoReplyEnabled bool
	RequireApproval  bool
	MaxEmailsPerRun  int

	// Safety
	IgnoreOwnEmails       bool
	IgnoreNoReplyEmails   bool
	IgnoreAutomatedEmails bool
	MaxEmailBodyLength    int

	// Reply Identity & Signature
	ReplyName      string
	ReplyTitle     string
	CompanyName    string
	ReplyContact   string
	EmailSignature string

	// Storage
	DBPath string
}

// Load reads configuration from .env file (if present) and environment variables,
// and validates that all mandatory parameters are set.
func Load() (*Config, error) {
	// Attempt to load .env; it is optional in production containerized setups.
	_ = godotenv.Load()

	cfg := &Config{
		AppName:                 getEnv("APP_NAME", "ai-email-responder"),
		AppEnv:                  getEnv("APP_ENV", "development"),
		LogLevel:                strings.ToLower(getEnv("LOG_LEVEL", "info")),
		PollIntervalSeconds:     getEnvAsInt("POLL_INTERVAL_SECONDS", 30),
		GmailEmail:              getEnv("GMAIL_EMAIL", ""),
		GmailAppPassword:        getEnv("GMAIL_APP_PASSWORD", ""),
		IMAPHost:                getEnv("IMAP_HOST", "imap.gmail.com"),
		IMAPPort:                getEnvAsInt("IMAP_PORT", 993),
		IMAPMailbox:             getEnv("IMAP_MAILBOX", "INBOX"),
		SMTPHost:                getEnv("SMTP_HOST", "smtp.gmail.com"),
		SMTPPort:                getEnvAsInt("SMTP_PORT", 587),
		OpenRouterAPIKey:        getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:         getEnv("OPENROUTER_MODEL", ""),
		OpenRouterBaseURL:       getEnv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
		AISystemInstructionFile: getEnv("AI_SYSTEM_INSTRUCTION_FILE", "prompts/system.txt"),
		AIUserInfoFile:          getEnv("AI_USER_INFO_FILE", "prompts/who_is_user.txt"),
		AITemperature:           getEnvAsFloat("AI_TEMPERATURE", 0.3),
		AIMaxTokens:             getEnvAsInt("AI_MAX_TOKENS", 500),
		AutoReplyEnabled:        getEnvAsBool("AUTO_REPLY_ENABLED", false),
		RequireApproval:         getEnvAsBool("REQUIRE_APPROVAL", true),
		MaxEmailsPerRun:         getEnvAsInt("MAX_EMAILS_PER_RUN", 10),
		IgnoreOwnEmails:         getEnvAsBool("IGNORE_OWN_EMAILS", true),
		IgnoreNoReplyEmails:     getEnvAsBool("IGNORE_NO_REPLY_EMAILS", true),
		IgnoreAutomatedEmails:   getEnvAsBool("IGNORE_AUTOMATED_EMAILS", true),
		MaxEmailBodyLength:      getEnvAsInt("MAX_EMAIL_BODY_LENGTH", 10000),
		ReplyName:               getEnv("REPLY_NAME", "Email Assistant"),
		ReplyTitle:              getEnv("REPLY_TITLE", ""),
		CompanyName:             getEnv("COMPANY_NAME", ""),
		ReplyContact:            getEnv("REPLY_CONTACT", ""),
		EmailSignature:          getEnv("EMAIL_SIGNATURE", ""),
		DBPath:                  getEnv("DB_PATH", "processed_emails.db"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// FormattedSignature generates the full email signature block based on configuration.
func (c *Config) FormattedSignature() string {
	if strings.TrimSpace(c.EmailSignature) != "" {
		return strings.TrimSpace(c.EmailSignature)
	}

	var lines []string
	if strings.TrimSpace(c.ReplyName) != "" {
		lines = append(lines, strings.TrimSpace(c.ReplyName))
	}
	if strings.TrimSpace(c.ReplyTitle) != "" {
		lines = append(lines, strings.TrimSpace(c.ReplyTitle))
	}
	if strings.TrimSpace(c.CompanyName) != "" {
		lines = append(lines, strings.TrimSpace(c.CompanyName))
	}
	if strings.TrimSpace(c.ReplyContact) != "" {
		lines = append(lines, strings.TrimSpace(c.ReplyContact))
	}

	if len(lines) == 0 {
		return "Best regards,\nEmail Assistant"
	}

	return "Best regards,\n" + strings.Join(lines, "\n")
}

// Validate checks for the presence of all required configuration settings.
// It explicitly never prints values of secret fields.
func (c *Config) Validate() error {
	var missing []string

	if strings.TrimSpace(c.GmailEmail) == "" {
		missing = append(missing, "GMAIL_EMAIL")
	}
	if strings.TrimSpace(c.GmailAppPassword) == "" {
		missing = append(missing, "GMAIL_APP_PASSWORD")
	}
	if strings.TrimSpace(c.OpenRouterAPIKey) == "" {
		missing = append(missing, "OPENROUTER_API_KEY")
	}
	if strings.TrimSpace(c.OpenRouterModel) == "" {
		missing = append(missing, "OPENROUTER_MODEL")
	}

	if len(missing) > 0 {
		return errors.New("Missing required environment variable: " + strings.Join(missing, ", "))
	}

	if c.PollIntervalSeconds <= 0 {
		c.PollIntervalSeconds = 30
	}
	if c.MaxEmailsPerRun <= 0 {
		c.MaxEmailsPerRun = 10
	}
	if c.MaxEmailBodyLength <= 0 {
		c.MaxEmailBodyLength = 10000
	}

	return nil
}

// CanAutoSend returns true only if auto reply is explicitly enabled and approval is not required.
func (c *Config) CanAutoSend() bool {
	return c.AutoReplyEnabled && !c.RequireApproval
}

// RedactedSummary returns a safe, non-sensitive string representation of the active configuration.
func (c *Config) RedactedSummary() string {
	var autoReplyStatus string
	if c.AutoReplyEnabled {
		autoReplyStatus = "ENABLED"
	} else {
		autoReplyStatus = "DISABLED"
	}

	var approvalStatus string
	if c.RequireApproval {
		approvalStatus = "YES"
	} else {
		approvalStatus = "NO"
	}

	return fmt.Sprintf("Environment: %s\nAI Model: %s\nIMAP: %s:%d (Mailbox: %s)\nSMTP: %s:%d\nAuto Reply: %s\nApproval Required: %s",
		c.AppEnv, c.OpenRouterModel, c.IMAPHost, c.IMAPPort, c.IMAPMailbox, c.SMTPHost, c.SMTPPort, autoReplyStatus, approvalStatus)
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(val)
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	valStr := getEnv(key, "")
	if val, err := strconv.Atoi(valStr); err == nil {
		return val
	}
	return fallback
}

func getEnvAsFloat(key string, fallback float64) float64 {
	valStr := getEnv(key, "")
	if val, err := strconv.ParseFloat(valStr, 64); err == nil {
		return val
	}
	return fallback
}

func getEnvAsBool(key string, fallback bool) bool {
	valStr := getEnv(key, "")
	if val, err := strconv.ParseBool(valStr); err == nil {
		return val
	}
	return fallback
}
