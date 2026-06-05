package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type TelegramClient struct {
	botToken      string
	defaultChatID string
	httpClient    *http.Client
}

func NewTelegramClient(botToken, defaultChatID string) *TelegramClient {
	return &TelegramClient{
		botToken:      strings.TrimSpace(botToken),
		defaultChatID: strings.TrimSpace(defaultChatID),
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *TelegramClient) Enabled() bool {
	return t.botToken != ""
}

func (t *TelegramClient) SendMessage(ctx context.Context, chatID, text string) error {
	if t.botToken == "" {
		return nil
	}
	if strings.TrimSpace(chatID) == "" {
		chatID = t.defaultChatID
	}
	if strings.TrimSpace(chatID) == "" {
		return nil
	}
	payload := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram sendMessage returned status %d", resp.StatusCode)
	}
	return nil
}

func EscapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}
