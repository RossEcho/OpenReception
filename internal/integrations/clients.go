package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
)

type OpenRouter struct {
	APIKey        string
	BaseURL       string
	Model         string
	FallbackModel string
	HTTPReferer   string
	AppName       string
	Timeout       time.Duration
	MaxTokens     int
	Temperature   float64
	Client        *http.Client
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error any `json:"error"`
}

func (o *OpenRouter) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if o.APIKey == "" || o.Model == "" {
		return "", errors.New("OpenRouter is not configured")
	}
	models := []string{o.Model}
	if o.FallbackModel != "" && o.FallbackModel != o.Model {
		models = append(models, o.FallbackModel)
	}
	var lastErr error
	for i, model := range models {
		text, status, err := o.request(ctx, model, systemPrompt, userPrompt)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if status != http.StatusTooManyRequests || i == len(models)-1 {
			break
		}
	}
	return "", lastErr
}

func (o *OpenRouter) request(ctx context.Context, model, systemPrompt, userPrompt string) (string, int, error) {
	body := map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userPrompt}},
		"max_tokens":  o.MaxTokens,
		"temperature": o.Temperature,
		"reasoning":   map[string]bool{"enabled": false},
	}
	data, _ := json.Marshal(body)
	requestCtx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(o.BaseURL, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if o.AppName != "" {
		req.Header.Set("X-Title", o.AppName)
	}
	if o.HTTPReferer != "" {
		req.Header.Set("HTTP-Referer", o.HTTPReferer)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	var decoded chatResponse
	_ = json.Unmarshal(raw, &decoded)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, fmt.Errorf("OpenRouter HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", resp.StatusCode, errors.New("OpenRouter returned an empty response")
	}
	return strings.TrimSpace(decoded.Choices[0].Message.Content), resp.StatusCode, nil
}

func (o *OpenRouter) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{Timeout: o.Timeout}
}

type WhatsApp struct {
	AccessToken   string
	GraphVersion  string
	PhoneNumberID string
	BaseURL       string
	Timeout       time.Duration
	Client        *http.Client
}

func (w *WhatsApp) SendText(ctx context.Context, to, text, replyTo string) (string, error) {
	body := map[string]any{
		"messaging_product": "whatsapp", "recipient_type": "individual", "to": to, "type": "text",
		"text": map[string]any{"preview_url": false, "body": truncateRunes(text, 4096)},
	}
	if replyTo != "" {
		body["context"] = map[string]string{"message_id": replyTo}
	}
	return w.sendMessage(ctx, body)
}

func (w *WhatsApp) SendImage(ctx context.Context, to, mediaID, caption, replyTo string) (string, error) {
	image := map[string]any{"id": mediaID}
	if strings.TrimSpace(caption) != "" {
		image["caption"] = truncateRunes(caption, 1024)
	}
	body := map[string]any{
		"messaging_product": "whatsapp", "recipient_type": "individual", "to": to, "type": "image", "image": image,
	}
	if replyTo != "" {
		body["context"] = map[string]string{"message_id": replyTo}
	}
	return w.sendMessage(ctx, body)
}

func (w *WhatsApp) UploadMedia(ctx context.Context, fileName, mimeType string, content io.Reader) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("messaging_product", "whatsapp")
	_ = writer.WriteField("type", mimeType)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filepath.Base(fileName)))
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, content); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(w.baseURL(), "/")+"/"+w.GraphVersion+"/"+w.PhoneNumberID+"/media", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+w.AccessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := w.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("WhatsApp media upload HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.ID == "" {
		return "", errors.New("WhatsApp media upload returned no media ID")
	}
	return decoded.ID, nil
}

func (w *WhatsApp) DeleteMedia(ctx context.Context, mediaID string) error {
	if strings.TrimSpace(mediaID) == "" {
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodDelete, strings.TrimRight(w.baseURL(), "/")+"/"+w.GraphVersion+"/"+mediaID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+w.AccessToken)
	resp, err := w.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("WhatsApp media delete HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (w *WhatsApp) DownloadMedia(ctx context.Context, mediaID string, maxBytes int64) ([]byte, string, error) {
	if strings.TrimSpace(mediaID) == "" {
		return nil, "", errors.New("WhatsApp media ID is required")
	}
	requestCtx, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()
	metadataRequest, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(w.baseURL(), "/")+"/"+w.GraphVersion+"/"+mediaID, nil)
	if err != nil {
		return nil, "", err
	}
	metadataRequest.Header.Set("Authorization", "Bearer "+w.AccessToken)
	metadataResponse, err := w.client().Do(metadataRequest)
	if err != nil {
		return nil, "", err
	}
	metadataRaw, _ := io.ReadAll(io.LimitReader(metadataResponse.Body, 1<<20))
	_ = metadataResponse.Body.Close()
	if metadataResponse.StatusCode < 200 || metadataResponse.StatusCode >= 300 {
		return nil, "", fmt.Errorf("WhatsApp media metadata HTTP %d", metadataResponse.StatusCode)
	}
	var metadata struct {
		URL      string `json:"url"`
		MIMEType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	}
	if json.Unmarshal(metadataRaw, &metadata) != nil || metadata.URL == "" {
		return nil, "", errors.New("WhatsApp media metadata was incomplete")
	}
	if maxBytes > 0 && metadata.FileSize > maxBytes {
		return nil, "", errors.New("WhatsApp media is too large")
	}
	downloadRequest, err := http.NewRequestWithContext(requestCtx, http.MethodGet, metadata.URL, nil)
	if err != nil {
		return nil, "", err
	}
	downloadRequest.Header.Set("Authorization", "Bearer "+w.AccessToken)
	downloadResponse, err := w.client().Do(downloadRequest)
	if err != nil {
		return nil, "", err
	}
	defer downloadResponse.Body.Close()
	if downloadResponse.StatusCode < 200 || downloadResponse.StatusCode >= 300 {
		return nil, "", fmt.Errorf("WhatsApp media download HTTP %d", downloadResponse.StatusCode)
	}
	if maxBytes < 1 {
		maxBytes = 5 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(downloadResponse.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > maxBytes {
		return nil, "", errors.New("WhatsApp media is too large")
	}
	return raw, metadata.MIMEType, nil
}

func (w *WhatsApp) sendMessage(ctx context.Context, body map[string]any) (string, error) {
	data, _ := json.Marshal(body)
	requestCtx, cancel := context.WithTimeout(ctx, w.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(w.baseURL(), "/")+"/"+w.GraphVersion+"/"+w.PhoneNumberID+"/messages", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+w.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("WhatsApp HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Messages) == 0 {
		return "", errors.New("WhatsApp returned no message ID")
	}
	return decoded.Messages[0].ID, nil
}

func (w *WhatsApp) baseURL() string {
	if strings.TrimSpace(w.BaseURL) == "" {
		return "https://graph.facebook.com"
	}
	return strings.TrimRight(w.BaseURL, "/")
}

func (w *WhatsApp) client() *http.Client {
	if w.Client != nil {
		return w.Client
	}
	return &http.Client{Timeout: w.Timeout}
}

func truncateRunes(value string, max int) string {
	r := []rune(value)
	if len(r) <= max {
		return value
	}
	return string(r[:max])
}
