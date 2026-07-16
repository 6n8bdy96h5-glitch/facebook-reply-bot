package main

import "testing"

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
