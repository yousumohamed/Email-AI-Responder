package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-email-responder/config"
)

func TestOpenRouterClient_GenerateReply_Success(t *testing.T) {
	mockResponse := `{
		"id": "gen-12345",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello,\n\nThank you for reaching out. We will get back to you shortly.\n\nBest regards,\nSupport"
				},
				"finish_reason": "stop"
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-api-key" {
			t.Errorf("unexpected Authorization header: %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	cfg := &config.Config{
		OpenRouterAPIKey:  "test-api-key",
		OpenRouterModel:   "google/gemini-flash",
		OpenRouterBaseURL: server.URL,
		AITemperature:     0.3,
		AIMaxTokens:       250,
	}

	client, err := NewOpenRouterClient(cfg)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	reply, err := client.GenerateReply(context.Background(), "user@example.com", "Question", "Can you help?")
	if err != nil {
		t.Fatalf("GenerateReply failed: %v", err)
	}

	if !strings.Contains(reply, "Thank you for reaching out") {
		t.Errorf("unexpected reply text: %s", reply)
	}
}

func TestOpenRouterClient_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": {"message": "Rate limit exceeded"}}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		OpenRouterAPIKey:  "test-api-key",
		OpenRouterModel:   "google/gemini-flash",
		OpenRouterBaseURL: server.URL,
	}

	client, _ := NewOpenRouterClient(cfg)
	_, err := client.GenerateReply(context.Background(), "user@example.com", "Question", "Can you help?")
	if err == nil {
		t.Fatal("expected error on rate limit, got nil")
	}

	if !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("expected error to mention rate limit, got: %v", err)
	}
}

func TestOpenRouterClient_EmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices": [{"message": {"role": "assistant", "content": "   "}}]}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		OpenRouterAPIKey:  "test-api-key",
		OpenRouterModel:   "google/gemini-flash",
		OpenRouterBaseURL: server.URL,
	}

	client, _ := NewOpenRouterClient(cfg)
	_, err := client.GenerateReply(context.Background(), "user@example.com", "Question", "Can you help?")
	if err == nil {
		t.Fatal("expected error on empty AI content, got nil")
	}
}
