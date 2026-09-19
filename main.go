package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"ai-email-responder/ai"
	"ai-email-responder/config"
	"ai-email-responder/email"
	"ai-email-responder/safety"
	"ai-email-responder/storage"
)

// App orchestrates the email monitoring, AI response generation, and reply delivery.
type App struct {
	cfg         *config.Config
	logger      *slog.Logger
	storage     *storage.Storage
	reader      email.Reader
	sender      email.Sender
	aiClient    ai.Client
	validator   *safety.Validator
	sessionSeen map[string]bool
	sessionMu   sync.Mutex
}

// Custom text handler to produce clean timestamped logs [15:04:05] INFO ...
type cleanLogHandler struct {
	level slog.Level
}

func (h *cleanLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *cleanLogHandler) Handle(_ context.Context, r slog.Record) error {
	ts := r.Time.Format("15:04:05")
	var attrs strings.Builder
	r.Attrs(func(a slog.Attr) bool {
		// Strictly filter out any attribute keys that could hold secrets
		keyLower := strings.ToLower(a.Key)
		if strings.Contains(keyLower, "password") || strings.Contains(keyLower, "token") || strings.Contains(keyLower, "key") {
			attrs.WriteString(fmt.Sprintf(" %s=[REDACTED]", a.Key))
		} else {
			attrs.WriteString(fmt.Sprintf(" %s=%q", a.Key, a.Value.String()))
		}
		return true
	})

	attrStr := attrs.String()
	if attrStr != "" {
		fmt.Fprintf(os.Stdout, "[%s] %s%s\n", ts, r.Message, attrStr)
	} else {
		fmt.Fprintf(os.Stdout, "[%s] %s\n", ts, r.Message)
	}
	return nil
}

func (h *cleanLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *cleanLogHandler) WithGroup(name string) slog.Handler {
	return h
}

func setupLogger(levelStr string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(levelStr) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := &cleanLogHandler{level: lvl}
	return slog.New(handler)
}

func main() {
	// 1. Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error:\n%v\n", err)
		os.Exit(1)
	}

	// 2. Initialize logger
	logger := setupLogger(cfg.LogLevel)
	logger.Info("Application started", slog.String("app", cfg.AppName), slog.String("env", cfg.AppEnv))

	// 3. Initialize SQLite storage
	store, err := storage.NewStorage(cfg.DBPath)
	if err != nil {
		logger.Error("Failed to initialize storage", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			logger.Error("Error closing storage", slog.String("error", closeErr.Error()))
		}
	}()

	// 4. Initialize AI OpenRouter Client
	aiClient, err := ai.NewOpenRouterClient(cfg)
	if err != nil {
		logger.Error("Failed to initialize AI client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// 5. Initialize Safety Validator
	validator := safety.NewValidator(cfg)

	// 6. Initialize Email Reader & Sender
	reader := email.NewIMAPReader(cfg)
	sender := email.NewSMTPSender(cfg)

	app := &App{
		cfg:         cfg,
		logger:      logger,
		storage:     store,
		reader:      reader,
		sender:      sender,
		aiClient:    aiClient,
		validator:   validator,
		sessionSeen: make(map[string]bool),
	}

	// 7. Setup graceful shutdown context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 8. Run application
	if err := app.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("Application terminated with error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("Application exited cleanly")
}

// Run executes the application startup sequence and main polling loop.
func (a *App) Run(ctx context.Context) error {
	// Verify IMAP connectivity on startup
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	imapStatus := "Connected"
	if err := a.reader.Connect(connectCtx); err != nil {
		a.logger.Warn("Initial IMAP connection deferred", slog.String("reason", err.Error()))
		imapStatus = "Connection Pending"
	} else {
		a.logger.Info("Connected to IMAP")
	}

	// Display startup banner (Section 28)
	a.displayStartupBanner(imapStatus)

	// Polling loop ticker
	pollInterval := time.Duration(a.cfg.PollIntervalSeconds) * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Perform initial check immediately
	a.pollInbox(ctx)

	for {
		select {
		case <-ctx.Done():
			a.logger.Info("Shutdown signal received, stopping email monitoring...")
			// Close reader connection
			if err := a.reader.Close(); err != nil {
				a.logger.Warn("Error closing IMAP connection", slog.String("error", err.Error()))
			}
			return nil

		case <-ticker.C:
			a.pollInbox(ctx)
		}
	}
}

// displayStartupBanner prints the CLI banner according to Section 28 without revealing secrets.
func (a *App) displayStartupBanner(imapStatus string) {
	autoReplyStatus := "DISABLED"
	if a.cfg.AutoReplyEnabled {
		autoReplyStatus = "ENABLED"
	}

	approvalStatus := "NO"
	if a.cfg.RequireApproval {
		approvalStatus = "YES"
	}

	fmt.Println("========================================")
	fmt.Println("        AI EMAIL AUTO-RESPONDER        ")
	fmt.Println("========================================")
	fmt.Printf("Environment: %s\n", a.cfg.AppEnv)
	fmt.Printf("AI Model: %s\n", a.cfg.OpenRouterModel)
	fmt.Printf("IMAP: %s\n", imapStatus)
	fmt.Println("SMTP: Configured")
	fmt.Printf("Auto Reply: %s\n", autoReplyStatus)
	fmt.Printf("Approval Required: %s\n", approvalStatus)
	fmt.Println("")
	fmt.Println("Waiting for new emails...")
	fmt.Println("========================================")
}

// pollInbox fetches and processes new incoming emails.
func (a *App) pollInbox(ctx context.Context) {
	a.logger.Debug("Checking inbox")

	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	isProcessedFunc := func(messageID string) bool {
		a.sessionMu.Lock()
		seen := a.sessionSeen[messageID]
		a.sessionMu.Unlock()
		if seen {
			return true
		}

		already, err := a.storage.IsProcessed(ctx, messageID)
		return err == nil && already
	}

	messages, err := a.reader.FetchUnread(fetchCtx, a.cfg.MaxEmailsPerRun, isProcessedFunc)
	if err != nil {
		a.logger.Error("Failed to fetch unread emails", slog.String("error", err.Error()))
		return
	}

	if len(messages) == 0 {
		return
	}

	for _, msg := range messages {
		// Stop processing if application context was canceled
		if ctx.Err() != nil {
			return
		}

		a.processSingleEmail(ctx, msg)
	}
}

// processSingleEmail executes the end-to-end pipeline for an individual email.
func (a *App) processSingleEmail(ctx context.Context, msg *email.Message) {
	// 1. In-session and persistent duplicate check
	a.sessionMu.Lock()
	seen := a.sessionSeen[msg.ID]
	a.sessionMu.Unlock()
	if seen {
		return
	}

	alreadyResponded, err := a.storage.IsProcessed(ctx, msg.ID)
	if err != nil {
		a.logger.Error("Database error checking duplicate email", slog.String("message_id", msg.ID), slog.String("error", err.Error()))
		return
	}
	if alreadyResponded {
		a.logger.Debug("Skipping already responded/ignored email", slog.String("message_id", msg.ID))
		return
	}

	// 2. Email filtering rules
	filterResult := email.CheckFilters(msg, a.cfg.GmailEmail, a.cfg.IgnoreOwnEmails, a.cfg.IgnoreNoReplyEmails, a.cfg.IgnoreAutomatedEmails)
	if filterResult.ShouldIgnore {
		a.logger.Info("Email ignored",
			slog.String("message_id", msg.ID),
			slog.String("from", msg.From),
			slog.String("reason", filterResult.Reason),
		)
		_ = a.storage.Record(ctx, msg.ID, msg.From, msg.Subject, "ignored")
		return
	}

	// 3. New email detected
	a.logger.Info("New email detected", slog.String("message_id", msg.ID))
	a.logger.Info(fmt.Sprintf("From: %s", msg.From))
	a.logger.Info(fmt.Sprintf("Subject: %s", msg.Subject))

	// 4. Generate AI response via OpenRouter
	a.logger.Info("Sending email content to OpenRouter")
	aiCtx, aiCancel := context.WithTimeout(ctx, 45*time.Second)
	defer aiCancel()

	replyText, err := a.aiClient.GenerateReply(aiCtx, msg.From, msg.Subject, msg.CleanBody)
	if err != nil {
		a.logger.Error("AI generation failed",
			slog.String("message_id", msg.ID),
			slog.String("error", err.Error()),
		)
		_ = a.storage.Record(ctx, msg.ID, msg.From, msg.Subject, "failed")
		return
	}
	a.logger.Info("AI response received")

	// 5. Apply signature & clean placeholder brackets
	replyText = email.ApplySignatureAndCleanPlaceholders(replyText, a.cfg)

	// 6. Safety / Policy validation
	if valErr := a.validator.Validate(replyText); valErr != nil {
		a.logger.Error("AI response validation failed",
			slog.String("message_id", msg.ID),
			slog.String("error", valErr.Error()),
		)
		_ = a.storage.Record(ctx, msg.ID, msg.From, msg.Subject, "failed")
		return
	}
	a.logger.Info("AI response validation passed")

	// 6. Prepare proposed reply
	recipient := email.DetermineRecipient(msg)
	replySubject := email.FormatReplySubject(msg.Subject)

	reply := &email.Reply{
		To:         recipient,
		Subject:    replySubject,
		Body:       replyText,
		InReplyTo:  msg.ID,
		References: msg.ID,
		Date:       time.Now(),
	}

	// 7. Display generated email reply in terminal
	fmt.Println("==============================")
	if a.cfg.CanAutoSend() {
		fmt.Println("GENERATED EMAIL REPLY (AUTO-SEND)")
	} else {
		fmt.Println("PROPOSED EMAIL REPLY")
	}
	fmt.Println("==============================")
	fmt.Printf("To: %s\n", reply.To)
	fmt.Printf("Subject: %s\n\n", reply.Subject)
	fmt.Println(reply.Body)
	fmt.Println("==============================")

	// 8. Sending decision: Automated Send vs. Interactive Approval vs. Preview Mode
	shouldSend := false
	if a.cfg.CanAutoSend() {
		// Fully automated sending
		shouldSend = true
	} else if a.cfg.RequireApproval {
		fmt.Print("Approve and send this reply via Gmail SMTP now? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "y" || input == "yes" {
			shouldSend = true
		} else {
			fmt.Println("Sending skipped.")
			a.logger.Info("Reply skipped by user approval")
			a.sessionMu.Lock()
			a.sessionSeen[msg.ID] = true
			a.sessionMu.Unlock()
			_ = a.storage.Record(ctx, msg.ID, msg.From, msg.Subject, "processed")
			return
		}
	} else {
		fmt.Println("Sending disabled.")
		a.logger.Info("Auto reply disabled")
		a.logger.Info("Reply NOT sent")
		a.sessionMu.Lock()
		a.sessionSeen[msg.ID] = true
		a.sessionMu.Unlock()
		_ = a.storage.Record(ctx, msg.ID, msg.From, msg.Subject, "processed")
		return
	}

	if shouldSend {
		// Send reply via Gmail SMTP
		a.logger.Info("Sending reply through Gmail SMTP")
		smtpCtx, smtpCancel := context.WithTimeout(ctx, 30*time.Second)
		defer smtpCancel()

		if sendErr := a.sender.Send(smtpCtx, reply); sendErr != nil {
			a.logger.Error("Failed to send reply through SMTP",
				slog.String("message_id", msg.ID),
				slog.String("error", sendErr.Error()),
			)
			_ = a.storage.Record(ctx, msg.ID, msg.From, msg.Subject, "failed")
			return
		}

		a.logger.Info("Reply sent successfully")

		// Mark email as sent in SQLite
		if recErr := a.storage.Record(ctx, msg.ID, msg.From, msg.Subject, "sent"); recErr != nil {
			a.logger.Error("Failed to record sent status in database", slog.String("error", recErr.Error()))
		} else {
			a.logger.Info("Email marked as processed")
		}

		// Mark email as read in IMAP mailbox
		if msg.SeqNum > 0 {
			_ = a.reader.MarkAsRead(ctx, msg.SeqNum)
		}
	}
}
