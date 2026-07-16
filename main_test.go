package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadResendConfig(t *testing.T) {
	t.Setenv("RESEND_API_KEY", " re_test_key ")
	t.Setenv("RESEND_FROM", " Messenger Bot <onboarding@resend.dev> ")
	t.Setenv("NOTIFY_EMAIL", " info98yy@gmail.com ")

	config := loadResendConfig()
	if !config.enabled() {
		t.Fatal("expected Resend configuration to be enabled")
	}
	if config.APIKey != "re_test_key" {
		t.Fatal("expected API key whitespace to be trimmed")
	}
	if config.From != "Messenger Bot <onboarding@resend.dev>" {
		t.Fatalf("unexpected sender: %q", config.From)
	}
	if config.NotifyTo != "info98yy@gmail.com" {
		t.Fatalf("unexpected recipient: %q", config.NotifyTo)
	}
}

func TestLoadResendConfigDefaultsSender(t *testing.T) {
	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("RESEND_FROM", "")
	t.Setenv("NOTIFY_EMAIL", "info98yy@gmail.com")

	config := loadResendConfig()
	if config.From != "Messenger Bot <onboarding@resend.dev>" {
		t.Fatalf("unexpected default sender: %q", config.From)
	}
}

func TestSendResendNotification(t *testing.T) {
	var received struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer re_secret" {
			t.Errorf("unexpected authorization header: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("unexpected content type: %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"email-id"}`))
	}))
	defer server.Close()

	config := resendConfig{
		APIKey:   "re_secret",
		From:     "Messenger Bot <onboarding@resend.dev>",
		NotifyTo: "info98yy@gmail.com",
	}
	if err := sendResendNotificationWithClient(config, "sender-123", "مرحبا", server.Client(), server.URL); err != nil {
		t.Fatalf("send notification: %v", err)
	}
	if received.From != config.From || len(received.To) != 1 || received.To[0] != config.NotifyTo {
		t.Fatalf("unexpected message routing: %+v", received)
	}
	if received.Subject != "رسالة Messenger جديدة" {
		t.Fatalf("unexpected subject: %q", received.Subject)
	}
	if !strings.Contains(received.Text, "sender-123") || !strings.Contains(received.Text, "مرحبا") {
		t.Fatalf("unexpected text: %q", received.Text)
	}
}

func TestSendResendNotificationReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
	}))
	defer server.Close()

	err := sendResendNotificationWithClient(resendConfig{APIKey: "re_secret", From: "from@example.com", NotifyTo: "to@example.com"}, "sender", "message", server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("expected API status error, got %v", err)
	}
	if strings.Contains(err.Error(), "re_secret") {
		t.Fatal("API key must not be present in error messages")
	}
}

func TestLoadSMTPConfig(t *testing.T) {
	t.Setenv("SMTP_HOST", " smtp.gmail.com ")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_USERNAME", " info98yy@gmail.com ")
	t.Setenv("SMTP_PASSWORD", "abcd efgh ijkl mnop")
	t.Setenv("NOTIFY_EMAIL", " info98yy@gmail.com ")

	config := loadSMTPConfig()
	if !config.enabled() {
		t.Fatal("expected SMTP configuration to be enabled")
	}
	if config.Host != "smtp.gmail.com" || config.Port != 465 {
		t.Fatalf("unexpected SMTP endpoint: %s:%d", config.Host, config.Port)
	}
	if config.Password != "abcdefghijklmnop" {
		t.Fatal("expected spaces to be removed from the app password")
	}
}

func TestLoadSMTPConfigRejectsInvalidPort(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.gmail.com")
	t.Setenv("SMTP_PORT", "invalid")
	t.Setenv("SMTP_USERNAME", "info98yy@gmail.com")
	t.Setenv("SMTP_PASSWORD", "abcdefghijklmnop")
	t.Setenv("NOTIFY_EMAIL", "info98yy@gmail.com")

	if config := loadSMTPConfig(); config.enabled() {
		t.Fatal("expected SMTP configuration with an invalid port to be disabled")
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("GRAPH_API_VERSION", " v24.0 ")
	if value := envOrDefault("GRAPH_API_VERSION", "v23.0"); value != "v24.0" {
		t.Fatalf("unexpected environment value: %q", value)
	}

	t.Setenv("GRAPH_API_VERSION", "")
	if value := envOrDefault("GRAPH_API_VERSION", "v23.0"); value != "v23.0" {
		t.Fatalf("unexpected fallback value: %q", value)
	}
}
