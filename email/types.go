package email

import (
	"time"
)

// Message represents an incoming email extracted from IMAP.
type Message struct {
	ID          string              // RFC 822 Message-ID
	SeqNum      uint32              // IMAP sequence number
	UID         uint32              // IMAP message UID
	From        string              // Sender address (e.g., "customer@example.com" or "Name <customer@example.com>")
	FromName    string              // Extracted friendly name
	FromEmail   string              // Extracted pure email address (lowercase)
	To          []string            // Recipient addresses
	ReplyTo     string              // Reply-To header if present
	Subject     string              // Email subject
	Date        time.Time           // Date header
	RawBody     string              // Original body (plain text or html)
	CleanBody   string              // Sanitized, truncated plain text body safe for AI prompt
	Headers     map[string][]string // All email headers
	IsTruncated bool                // Whether body exceeded MAX_EMAIL_BODY_LENGTH
}

// Reply represents an outgoing email response prepared by the application.
type Reply struct {
	To         string    // Destination address (Reply-To or From)
	Subject    string    // Subject line (prefixed with Re: if needed)
	Body       string    // Email text body
	InReplyTo  string    // Message-ID being replied to
	References string    // Thread references
	Date       time.Time // Sent timestamp
}

// FilterResult contains the outcome of evaluating an email against filtering rules.
type FilterResult struct {
	ShouldIgnore bool
	Reason       string
}
