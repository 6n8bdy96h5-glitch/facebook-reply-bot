package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/sashabaranov/go-openai"
)

var whatsappNotified = struct {
	sync.Mutex
	users map[string]time.Time
}{
	users: make(map[string]time.Time),
}

func shouldSendInitialWhatsApp(senderID string) bool {
	whatsappNotified.Lock()
	defer whatsappNotified.Unlock()

	now := time.Now()
	lastSent, exists := whatsappNotified.users[senderID]
	if exists && now.Sub(lastSent) < 24*time.Hour {
		return false
	}
	whatsappNotified.users[senderID] = now
	return true
}

func main() {
	// Load environment variables from .env if present
	_ = godotenv.Load()

	verifyToken := os.Getenv("VERIFY_TOKEN")
	pageAccessToken := os.Getenv("PAGE_ACCESS_TOKEN")
	openaiKey := os.Getenv("OPENAI_API_KEY")
	graphAPIVersion := envOrDefault("GRAPH_API_VERSION", "v24.0")
	resendConfig := loadResendConfig()
	mailConfig := loadSMTPConfig()
	whatsAppConfig := loadWhatsAppConfig(graphAPIVersion)

	if verifyToken == "" || pageAccessToken == "" {
		log.Fatal("VERIFY_TOKEN and PAGE_ACCESS_TOKEN must be set as environment variables")
	}

	r := gin.New()
	// Avoid logging request query strings because Meta sends the webhook
	// verification token as a query parameter.
	r.Use(gin.Recovery())
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("Failed to configure trusted proxies: %v", err)
	}
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Facebook webhook verification endpoint
	r.GET("/webhook", func(c *gin.Context) {
		mode := c.Query("hub.mode")
		token := c.Query("hub.verify_token")
		challenge := c.Query("hub.challenge")
		if mode == "subscribe" && token == verifyToken {
			c.String(http.StatusOK, challenge)
		} else {
			c.String(http.StatusForbidden, "Verification failed")
		}
	})

	// Webhook to receive messages
	r.POST("/webhook", func(c *gin.Context) {
		var payload map[string]interface{}
		if err := c.BindJSON(&payload); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		// Extract message and sender PSID
		entryList, ok := payload["entry"].([]interface{})
		if !ok || len(entryList) == 0 {
			c.Status(http.StatusOK)
			return
		}
		for _, entryRaw := range entryList {
			entry, _ := entryRaw.(map[string]interface{})
			messList, _ := entry["messaging"].([]interface{})
			for _, messRaw := range messList {
				mess, _ := messRaw.(map[string]interface{})
				sender, _ := mess["sender"].(map[string]interface{})
				psid, _ := sender["id"].(string)
				messageObj, ok := mess["message"].(map[string]interface{})
				if !ok {
					continue
				}
				text, _ := messageObj["text"].(string)
				// Generate reply
				replyText := "شكراً على رسالتك!" // default
				if openaiKey != "" {
					client := openai.NewClient(openaiKey)
					resp, err := client.CreateChatCompletion(c.Request.Context(), openai.ChatCompletionRequest{
						Model:    openai.GPT3Dot5Turbo,
						Messages: []openai.ChatCompletionMessage{{Role: "user", Content: text}},
					})
					if err == nil && len(resp.Choices) > 0 {
						replyText = resp.Choices[0].Message.Content
					}
				}
				// Send reply via Graph API
				if err := sendMessage(graphAPIVersion, pageAccessToken, psid, replyText); err != nil {
					log.Printf("Failed to send Messenger reply: %v", err)
				}

				if resendConfig.enabled() || mailConfig.enabled() || whatsAppConfig.enabled() {
					senderID, incomingText := psid, text
					go func() {
						var emailErr error
						if resendConfig.enabled() {
							emailErr = sendResendNotification(resendConfig, senderID, incomingText)
						} else if mailConfig.enabled() {
							emailErr = sendEmailNotification(mailConfig, senderID, incomingText)
						}
						if emailErr != nil {
							log.Printf("Failed to send email notification: %v", emailErr)
						}
						if whatsAppConfig.enabled() && shouldSendInitialWhatsApp(senderID) {
							if err := sendWhatsAppNotification(whatsAppConfig, senderID, incomingText); err != nil {
								log.Printf("Failed to send WhatsApp notification: %v", err)
							}
						}
					}()
				}
			}
		}
		c.Status(http.StatusOK)
	})

	// Run server on port 8080 (or PORT env var)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func sendMessage(graphAPIVersion, pageToken, recipientID, message string) error {
	endpoint, err := url.Parse("https://graph.facebook.com/" + graphAPIVersion + "/me/messages")
	if err != nil {
		return fmt.Errorf("build Graph API URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("access_token", pageToken)
	endpoint.RawQuery = query.Encode()

	payload := map[string]interface{}{
		"recipient": map[string]string{"id": recipientID},
		"message":   map[string]string{"text": message},
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Messenger payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create Messenger request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send Messenger request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Graph API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

const resendAPIEndpoint = "https://api.resend.com/emails"

type whatsAppConfig struct {
	AccessToken      string
	PhoneNumberID    string
	To               string
	GraphAPIVersion  string
	TemplateName     string
	TemplateLanguage string
}

func loadWhatsAppConfig(graphAPIVersion string) whatsAppConfig {
	return whatsAppConfig{
		AccessToken:      strings.TrimSpace(os.Getenv("WHATSAPP_ACCESS_TOKEN")),
		PhoneNumberID:    strings.TrimSpace(os.Getenv("WHATSAPP_PHONE_NUMBER_ID")),
		To:               strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(os.Getenv("WHATSAPP_TO")), " ", ""), "+"),
		GraphAPIVersion:  strings.TrimSpace(graphAPIVersion),
		TemplateName:     envOrDefault("WHATSAPP_TEMPLATE_NAME", "hello_world"),
		TemplateLanguage: envOrDefault("WHATSAPP_TEMPLATE_LANGUAGE", "en_US"),
	}
}

func (config whatsAppConfig) enabled() bool {
	return config.AccessToken != "" && config.PhoneNumberID != "" && config.To != "" && config.GraphAPIVersion != "" && config.TemplateName != "" && config.TemplateLanguage != ""
}

func sendWhatsAppNotification(config whatsAppConfig, senderID, incomingText string) error {
	endpoint, err := url.JoinPath("https://graph.facebook.com", config.GraphAPIVersion, config.PhoneNumberID, "messages")
	if err != nil {
		return fmt.Errorf("build WhatsApp Graph API URL: %w", err)
	}
	return sendWhatsAppNotificationWithClient(config, senderID, incomingText, &http.Client{Timeout: 15 * time.Second}, endpoint)
}

func sendWhatsAppNotificationWithClient(config whatsAppConfig, senderID, incomingText string, client *http.Client, endpoint string) error {
	template := map[string]interface{}{
		"name":     config.TemplateName,
		"language": map[string]string{"code": config.TemplateLanguage},
	}
	// Meta's built-in hello_world template has no variables. Custom notification
	// templates receive sender ID, contact, city, and the Messenger message.
	if config.TemplateName != "hello_world" {
		template["components"] = []map[string]interface{}{
			{
				"type": "body",
				"parameters": []map[string]string{
					{"type": "text", "text": senderID},
					{"type": "text", "text": "غير متوفر"},
					{"type": "text", "text": "غير متوفر"},
					{"type": "text", "text": incomingText},
				},
			},
		}
	}

	payload := map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                config.To,
		"type":              "template",
		"template":          template,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode WhatsApp payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create WhatsApp request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send WhatsApp request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("WhatsApp API returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

type resendConfig struct {
	APIKey   string
	From     string
	NotifyTo string
}

func loadResendConfig() resendConfig {
	return resendConfig{
		APIKey:   strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		From:     envOrDefault("RESEND_FROM", "Messenger Bot <onboarding@resend.dev>"),
		NotifyTo: strings.TrimSpace(os.Getenv("NOTIFY_EMAIL")),
	}
}

func (config resendConfig) enabled() bool {
	return config.APIKey != "" && config.From != "" && config.NotifyTo != ""
}

func sendResendNotification(config resendConfig, senderID, incomingText string) error {
	return sendResendNotificationWithClient(config, senderID, incomingText, &http.Client{Timeout: 15 * time.Second}, resendAPIEndpoint)
}

func sendResendNotificationWithClient(config resendConfig, senderID, incomingText string, client *http.Client, endpoint string) error {
	payload := struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}{
		From:    config.From,
		To:      []string{config.NotifyTo},
		Subject: "رسالة Messenger جديدة",
		Text:    fmt.Sprintf("وصلت رسالة جديدة من المعرّف %s:\n\n%s\n", senderID, incomingText),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Resend payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send Resend request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Resend API returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

type smtpConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	NotifyTo string
}

func loadSMTPConfig() smtpConfig {
	port, err := strconv.Atoi(envOrDefault("SMTP_PORT", "465"))
	if err != nil || port < 1 || port > 65535 {
		log.Printf("Invalid SMTP_PORT; email notifications are disabled")
		return smtpConfig{}
	}
	return smtpConfig{
		Host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
		Port:     port,
		Username: strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		Password: strings.ReplaceAll(strings.TrimSpace(os.Getenv("SMTP_PASSWORD")), " ", ""),
		NotifyTo: strings.TrimSpace(os.Getenv("NOTIFY_EMAIL")),
	}
}

func (config smtpConfig) enabled() bool {
	return config.Host != "" && config.Username != "" && config.Password != "" && config.NotifyTo != ""
}

func sendEmailNotification(config smtpConfig, senderID, incomingText string) error {
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host}

	var client *smtp.Client
	var err error
	if config.Port == 465 {
		connection, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", address, tlsConfig)
		if dialErr != nil {
			return fmt.Errorf("connect to SMTP over TLS: %w", dialErr)
		}
		client, err = smtp.NewClient(connection, config.Host)
	} else {
		client, err = smtp.Dial(address)
		if err == nil {
			err = client.StartTLS(tlsConfig)
		}
	}
	if err != nil {
		return fmt.Errorf("initialize SMTP client: %w", err)
	}
	defer client.Close()

	if err := client.Auth(smtp.PlainAuth("", config.Username, config.Password, config.Host)); err != nil {
		return fmt.Errorf("authenticate to SMTP: %w", err)
	}
	if err := client.Mail(config.Username); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(config.NotifyTo); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message body: %w", err)
	}
	subject := mime.QEncoding.Encode("UTF-8", "رسالة Messenger جديدة")
	body := fmt.Sprintf("وصلت رسالة جديدة من المعرّف %s:\r\n\r\n%s\r\n", senderID, incomingText)
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", config.Username, config.NotifyTo, subject, body)
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("close SMTP session: %w", err)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
