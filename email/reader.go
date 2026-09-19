package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-email-responder/config"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	mailMsg "github.com/emersion/go-message/mail"
)

// Reader defines the operations needed to read emails.
type Reader interface {
	Connect(ctx context.Context) error
	FetchUnread(ctx context.Context, limit int, isProcessed func(messageID string) bool) ([]*Message, error)
	MarkAsRead(ctx context.Context, seqNum uint32) error
	Close() error
}

// IMAPReader implements Reader for Gmail IMAP.
type IMAPReader struct {
	cfg        *config.Config
	client     *client.Client
	mu         sync.Mutex
	lastActive time.Time
}

// NewIMAPReader creates a new IMAPReader instance.
func NewIMAPReader(cfg *config.Config) *IMAPReader {
	return &IMAPReader{
		cfg: cfg,
	}
}

// Connect establishes a TLS connection to Gmail IMAP and logs in.
func (r *IMAPReader) Connect(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.connectLocked(ctx)
}

func (r *IMAPReader) connectLocked(ctx context.Context) error {
	if r.client != nil {
		// Close existing client if any
		_ = r.client.Logout()
		_ = r.client.Close()
		r.client = nil
	}

	addr := fmt.Sprintf("%s:%d", r.cfg.IMAPHost, r.cfg.IMAPPort)
	tlsConfig := &tls.Config{
		ServerName: r.cfg.IMAPHost,
		MinVersion: tls.VersionTLS12,
	}

	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}
	tlsConn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to IMAP server %s: %w", addr, err)
	}

	c, err := client.New(tlsConn)
	if err != nil {
		_ = tlsConn.Close()
		return fmt.Errorf("failed to create IMAP client: %w", err)
	}

	// Hard command timeout so broken or dropped sockets immediately fail within seconds rather than 15 minutes
	c.Timeout = 10 * time.Second

	// Silence internal background transport error logs (e.g. idle socket disconnects)
	c.ErrorLog = log.New(io.Discard, "", 0)

	// Login using Gmail address and App Password
	if err := c.Login(r.cfg.GmailEmail, r.cfg.GmailAppPassword); err != nil {
		_ = c.Close()
		return fmt.Errorf("failed to authenticate with IMAP server: %w", err)
	}

	// Select inbox
	_, err = c.Select(r.cfg.IMAPMailbox, false)
	if err != nil {
		_ = c.Logout()
		_ = c.Close()
		return fmt.Errorf("failed to select mailbox %s: %w", r.cfg.IMAPMailbox, err)
	}

	r.client = c
	r.lastActive = time.Now()
	return nil
}

// ensureConnected verifies client state or reconnects.
func (r *IMAPReader) ensureConnected(ctx context.Context) error {
	if r.client == nil || r.client.State() != imap.SelectedState {
		return r.connectLocked(ctx)
	}
	// Ping connection with Noop to detect and recover from idle socket drops
	if err := r.client.Noop(); err != nil {
		return r.connectLocked(ctx)
	}
	r.lastActive = time.Now()
	return nil
}

// FetchUnread retrieves unread messages from the inbox up to the specified limit, prioritizing newest messages
// and filtering out messages that have already been processed.
func (r *IMAPReader) FetchUnread(ctx context.Context, limit int, isProcessed func(messageID string) bool) ([]*Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ensureConnected(ctx); err != nil {
		return nil, fmt.Errorf("IMAP connection error: %w", err)
	}

	if limit <= 0 {
		limit = 10
	}

	// Search criteria: UNSEEN (unread) messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}

	seqNums, err := r.client.Search(criteria)
	if err != nil {
		// If search fails, try reconnecting once
		if recErr := r.connectLocked(ctx); recErr == nil {
			seqNums, err = r.client.Search(criteria)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to search unread messages: %w", err)
		}
	}

	if len(seqNums) == 0 {
		return nil, nil
	}

	// Sort sequence numbers in descending order so the NEWEST emails are inspected first
	sort.Slice(seqNums, func(i, j int) bool {
		return seqNums[i] > seqNums[j]
	})

	var targetSeqNums []uint32
	if isProcessed != nil {
		// Fetch lightweight envelopes first to filter out already-processed emails
		batchSize := 30
		for i := 0; i < len(seqNums) && len(targetSeqNums) < limit; i += batchSize {
			end := i + batchSize
			if end > len(seqNums) {
				end = len(seqNums)
			}
			batch := seqNums[i:end]

			seqSet := new(imap.SeqSet)
			seqSet.AddNum(batch...)

			items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid}
			envChan := make(chan *imap.Message, len(batch))
			done := make(chan error, 1)

			go func() {
				done <- r.client.Fetch(seqSet, items, envChan)
			}()

			envMap := make(map[uint32]*imap.Message)
			for m := range envChan {
				if m != nil && m.Envelope != nil {
					envMap[m.SeqNum] = m
				}
			}
			_ = <-done

			// Inspect in descending order
			for _, sn := range batch {
				if m, ok := envMap[sn]; ok {
					msgID := strings.TrimSpace(m.Envelope.MessageId)
					if msgID == "" || !isProcessed(msgID) {
						targetSeqNums = append(targetSeqNums, sn)
						if len(targetSeqNums) >= limit {
							break
						}
					}
				}
			}
		}
	} else {
		targetSeqNums = seqNums
		if len(targetSeqNums) > limit {
			targetSeqNums = targetSeqNums[:limit]
		}
	}

	if len(targetSeqNums) == 0 {
		return nil, nil
	}

	// Now fetch full body and headers for target unprocessed messages
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(targetSeqNums...)

	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, section.FetchItem()}

	msgChan := make(chan *imap.Message, len(targetSeqNums))
	done := make(chan error, 1)

	go func() {
		done <- r.client.Fetch(seqSet, items, msgChan)
	}()

	var messages []*Message
	for imapMsg := range msgChan {
		if imapMsg == nil || imapMsg.Envelope == nil {
			continue
		}

		bodyLiteral := imapMsg.GetBody(section)
		msg, parseErr := parseIMAPMessage(imapMsg, bodyLiteral, r.cfg.MaxEmailBodyLength)
		if parseErr != nil {
			continue
		}
		messages = append(messages, msg)
	}

	if err := <-done; err != nil {
		return messages, fmt.Errorf("error during fetch: %w", err)
	}

	// Order returned messages newest first
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].SeqNum > messages[j].SeqNum
	})

	return messages, nil
}

// parseIMAPMessage extracts relevant fields from an imap.Message.
func parseIMAPMessage(imapMsg *imap.Message, bodyReader io.Reader, maxBodyLength int) (*Message, error) {
	env := imapMsg.Envelope
	msg := &Message{
		ID:      strings.TrimSpace(env.MessageId),
		SeqNum:  imapMsg.SeqNum,
		UID:     imapMsg.Uid,
		Subject: env.Subject,
		Date:    env.Date,
		Headers: make(map[string][]string),
	}

	// Fallback Message-ID if missing
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("<generated-%d-%d@email-responder.local>", imapMsg.Uid, time.Now().UnixNano())
	}

	// Extract From address
	if len(env.From) > 0 {
		fromAddr := env.From[0]
		msg.FromName = fromAddr.PersonalName
		if fromAddr.HostName != "" {
			msg.FromEmail = strings.ToLower(fmt.Sprintf("%s@%s", fromAddr.MailboxName, fromAddr.HostName))
			msg.From = fmt.Sprintf("%s <%s>", fromAddr.PersonalName, msg.FromEmail)
		} else {
			msg.From = fromAddr.MailboxName
			msg.FromEmail = strings.ToLower(fromAddr.MailboxName)
		}
	}

	// Extract To addresses
	for _, toAddr := range env.To {
		if toAddr.HostName != "" {
			msg.To = append(msg.To, fmt.Sprintf("%s@%s", toAddr.MailboxName, toAddr.HostName))
		}
	}

	// Extract Reply-To address
	if len(env.ReplyTo) > 0 {
		rt := env.ReplyTo[0]
		if rt.HostName != "" {
			msg.ReplyTo = strings.ToLower(fmt.Sprintf("%s@%s", rt.MailboxName, rt.HostName))
		}
	}

	// Parse body and extract headers
	if bodyReader != nil {
		plainText, htmlText, headers, err := parseMIMEBody(bodyReader)
		if err == nil {
			msg.Headers = headers
			if plainText != "" {
				msg.RawBody = plainText
				msg.CleanBody, msg.IsTruncated = SanitizeAndTruncateBody(plainText, false, maxBodyLength)
			} else if htmlText != "" {
				msg.RawBody = htmlText
				msg.CleanBody, msg.IsTruncated = SanitizeAndTruncateBody(htmlText, true, maxBodyLength)
			}
		} else {
			// Fallback: read directly
			rawBytes, _ := io.ReadAll(bodyReader)
			msg.RawBody = string(rawBytes)
			msg.CleanBody, msg.IsTruncated = SanitizeAndTruncateBody(msg.RawBody, false, maxBodyLength)
		}
	}

	return msg, nil
}

// parseMIMEBody reads the MIME structure and separates text/plain from text/html, ignoring attachments.
func parseMIMEBody(r io.Reader) (string, string, map[string][]string, error) {
	headers := make(map[string][]string)

	mr, err := mailMsg.CreateReader(r)
	if err != nil {
		return "", "", headers, err
	}

	// Copy all top-level headers
	fields := mr.Header.Fields()
	for fields.Next() {
		k := fields.Key()
		v := fields.Value()
		headers[k] = append(headers[k], v)
	}

	var plainText, htmlText string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch h := part.Header.(type) {
		case *mailMsg.InlineHeader:
			contentType, _, _ := h.ContentType()
			bodyBytes, err := io.ReadAll(part.Body)
			if err != nil {
				continue
			}

			if strings.HasPrefix(strings.ToLower(contentType), "text/plain") {
				if plainText == "" {
					plainText = string(bodyBytes)
				}
			} else if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
				if htmlText == "" {
					htmlText = string(bodyBytes)
				}
			}
		case *mailMsg.AttachmentHeader:
			// Explicitly ignore attachments: never read attachment contents or pass them to AI
			continue
		}
	}

	return plainText, htmlText, headers, nil
}

// MarkAsRead marks a message as seen in the mailbox.
func (r *IMAPReader) MarkAsRead(ctx context.Context, seqNum uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ensureConnected(ctx); err != nil {
		return fmt.Errorf("IMAP connection error: %w", err)
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(seqNum)

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.SeenFlag}

	return r.client.Store(seqSet, item, flags, nil)
}

// Close gracefully logs out and terminates the IMAP connection.
func (r *IMAPReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.client != nil {
		_ = r.client.Logout()
		err := r.client.Close()
		r.client = nil
		return err
	}
	return nil
}
