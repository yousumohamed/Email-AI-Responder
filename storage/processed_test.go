package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStorage_DuplicateAndRecord(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_processed.db")

	s, err := NewStorage(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	msgID := "<test-12345@mail.example.com>"

	// 1. Initial check: not processed
	processed, err := s.IsProcessed(ctx, msgID)
	if err != nil {
		t.Fatalf("IsProcessed returned error: %v", err)
	}
	if processed {
		t.Errorf("expected msgID to be unrecorded initially")
	}

	// 2. Record as sent
	if err := s.Record(ctx, msgID, "customer@example.com", "Website Inquiry", "sent"); err != nil {
		t.Fatalf("failed to record: %v", err)
	}

	// 3. Check is now processed (since it was sent)
	processed, err = s.IsProcessed(ctx, msgID)
	if err != nil {
		t.Fatalf("IsProcessed returned error: %v", err)
	}
	if !processed {
		t.Errorf("expected msgID to be marked as processed when status is 'sent'")
	}

	rec, err := s.GetRecord(ctx, msgID)
	if err != nil {
		t.Fatalf("failed to get record: %v", err)
	}
	if rec.Status != "sent" {
		t.Errorf("expected status 'sent', got '%s'", rec.Status)
	}
	if rec.Sender != "customer@example.com" {
		t.Errorf("expected sender 'customer@example.com', got '%s'", rec.Sender)
	}

	// 5. Verify failed status does not prevent retry
	failedMsgID := "<failed-test@mail.example.com>"
	if err := s.Record(ctx, failedMsgID, "user@example.com", "Help", "failed"); err != nil {
		t.Fatalf("failed to record failed status: %v", err)
	}
	isProcessedFailed, err := s.IsProcessed(ctx, failedMsgID)
	if err != nil {
		t.Fatalf("IsProcessed error: %v", err)
	}
	if isProcessedFailed {
		t.Errorf("expected failed status to allow retry (IsProcessed == false)")
	}

	// Clean up verify
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Errorf("expected database file to exist at %s", dbPath)
	}
}
