package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ProcessedEmail represents a record in the processed_emails table.
type ProcessedEmail struct {
	ID          int64
	MessageID   string
	Sender      string
	Subject     string
	Status      string
	ProcessedAt time.Time
}

// Storage handles persistence of processed emails using SQLite.
type Storage struct {
	db *sql.DB
}

// NewStorage initializes SQLite at dbPath and creates the processed_emails table if missing.
func NewStorage(dbPath string) (*Storage, error) {
	if dbPath == "" {
		dbPath = "processed_emails.db"
	}

	// Ensure the parent directory exists if a path with directories is provided.
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database at %s: %w", dbPath, err)
	}

	// Set connection limits appropriate for SQLite to avoid lock contention
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS processed_emails (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_id TEXT UNIQUE NOT NULL,
		sender TEXT,
		subject TEXT,
		status TEXT NOT NULL,
		processed_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_processed_emails_message_id ON processed_emails(message_id);
	`
	if _, err := db.ExecContext(ctx, createTableSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize processed_emails table: %w", err)
	}

	return &Storage{db: db}, nil
}

// IsProcessed checks whether an email with the given messageID has already been responded to (sent) or filtered out (ignored).
func (s *Storage) IsProcessed(ctx context.Context, messageID string) (bool, error) {
	trimmedID := strings.TrimSpace(messageID)
	if trimmedID == "" {
		return false, nil
	}

	var count int
	query := `SELECT COUNT(1) FROM processed_emails WHERE message_id = ? AND status IN ('sent', 'ignored')`
	err := s.db.QueryRowContext(ctx, query, trimmedID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check if message_id is processed: %w", err)
	}

	return count > 0, nil
}

// Record inserts or updates a record for the specified messageID.
func (s *Storage) Record(ctx context.Context, messageID, sender, subject, status string) error {
	trimmedID := strings.TrimSpace(messageID)
	if trimmedID == "" {
		return fmt.Errorf("cannot record email with empty message_id")
	}

	query := `
	INSERT INTO processed_emails (message_id, sender, subject, status, processed_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(message_id) DO UPDATE SET
		sender = excluded.sender,
		subject = excluded.subject,
		status = excluded.status,
		processed_at = excluded.processed_at;
	`
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, query, trimmedID, sender, subject, status, now)
	if err != nil {
		return fmt.Errorf("failed to record processed email %s: %w", trimmedID, err)
	}

	return nil
}

// GetRecord retrieves the details of a processed email by its message ID.
func (s *Storage) GetRecord(ctx context.Context, messageID string) (*ProcessedEmail, error) {
	trimmedID := strings.TrimSpace(messageID)
	if trimmedID == "" {
		return nil, sql.ErrNoRows
	}

	query := `SELECT id, message_id, sender, subject, status, processed_at FROM processed_emails WHERE message_id = ?`
	row := s.db.QueryRowContext(ctx, query, trimmedID)

	var p ProcessedEmail
	err := row.Scan(&p.ID, &p.MessageID, &p.Sender, &p.Subject, &p.Status, &p.ProcessedAt)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

// Close gracefully closes the SQLite database connection.
func (s *Storage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
