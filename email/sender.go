package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"ai-email-responder/config"
)

// Sender defines the interface for sending email replies.
type Sender interface {
	Send(ctx context.Context, reply *Reply) error
}

// SMTPSender implements Sender using Gmail SMTP with STARTTLS.
type SMTPSender struct {
	cfg *config.Config
}

// NewSMTPSender creates a new SMTPSender instance.
func NewSMTPSender(cfg *config.Config) *SMTPSender {
	return &SMTPSender{
		cfg: cfg,
	}
}

// Send delivers an email reply using Gmail SMTP with STARTTLS.
func (s *SMTPSender) Send(ctx context.Context, reply *Reply) error {
	if reply == nil {
		return errors.New("cannot send nil reply")
	}

	toAddr := strings.TrimSpace(reply.To)
	if toAddr == "" {
		return errors.New("recipient address is empty")
	}

	fromAddr := strings.TrimSpace(s.cfg.GmailEmail)
	if fromAddr == "" {
		return errors.New("sender address (GMAIL_EMAIL) is empty")
	}

	fromDisplay := fromAddr
	if s.cfg.ReplyName != "" {
		fromDisplay = fmt.Sprintf("%s <%s>", s.cfg.ReplyName, fromAddr)
	}

	replyDate := reply.Date
	if replyDate.IsZero() {
		replyDate = time.Now()
	}

	// Format standard RFC 5322 email headers and message
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", fromDisplay))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", toAddr))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", reply.Subject))
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", replyDate.Format(time.RFC1123Z)))
	msg.WriteString(fmt.Sprintf("MIME-Version: 1.0\r\n"))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")

	if reply.InReplyTo != "" {
		msg.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", reply.InReplyTo))
	}
	if reply.References != "" {
		msg.WriteString(fmt.Sprintf("References: %s\r\n", reply.References))
	} else if reply.InReplyTo != "" {
		msg.WriteString(fmt.Sprintf("References: %s\r\n", reply.InReplyTo))
	}

	msg.WriteString("\r\n")
	msg.WriteString(reply.Body)

	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)

	// Establish connection with context deadline
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to dial SMTP server %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))

	// Wrap in smtp.Client
	c, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("failed to initialize SMTP client: %w", err)
	}
	defer c.Quit()

	// Check if STARTTLS is supported and upgrade connection
	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{
			ServerName: s.cfg.SMTPHost,
			MinVersion: tls.VersionTLS12,
		}
		if err = c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("SMTP STARTTLS failed: %w", err)
		}
	} else {
		return errors.New("SMTP server does not support STARTTLS")
	}

	// Authenticate using PLAIN auth with App Password
	auth := smtp.PlainAuth("", s.cfg.GmailEmail, s.cfg.GmailAppPassword, s.cfg.SMTPHost)
	if err = c.Auth(auth); err != nil {
		return fmt.Errorf("SMTP authentication failed: %w", err)
	}

	// Set sender
	if err = c.Mail(fromAddr); err != nil {
		return fmt.Errorf("SMTP MAIL command failed: %w", err)
	}

	// Set recipient
	if err = c.Rcpt(toAddr); err != nil {
		return fmt.Errorf("SMTP RCPT command failed: %w", err)
	}

	// Send message body
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}

	if _, err = w.Write([]byte(msg.String())); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to write email body: %w", err)
	}

	if err = w.Close(); err != nil {
		return fmt.Errorf("failed to finalize email message: %w", err)
	}

	return nil
}
