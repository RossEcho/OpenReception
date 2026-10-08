package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type diagnosticStatus struct {
	State     string
	Label     string
	Detail    string
	CheckedAt string
}

type systemDiagnostics struct {
	WhatsApp   diagnosticStatus
	OpenRouter diagnosticStatus
	Webhook    diagnosticStatus
}

type diagnosticsStore struct {
	mu    sync.RWMutex
	value systemDiagnostics
}

func initialDiagnostics() systemDiagnostics {
	checking := diagnosticStatus{State: "checking", Label: "Checking", Detail: "Validation starts when the server loads."}
	return systemDiagnostics{WhatsApp: checking, OpenRouter: checking, Webhook: checking}
}

func (d *diagnosticsStore) get() systemDiagnostics {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.value
}

func (d *diagnosticsStore) set(value systemDiagnostics) {
	d.mu.Lock()
	d.value = value
	d.mu.Unlock()
}

// StartDiagnostics validates external integrations immediately on startup and
// periodically afterwards. Checks are read-only and never log or expose keys.
func (s *Server) StartDiagnostics(ctx context.Context) {
	run := func() {
		checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		s.runDiagnostics(checkCtx)
	}
	go run()
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func (s *Server) runDiagnostics(ctx context.Context) systemDiagnostics {
	now := time.Now().In(s.config.Location).Format("2006-01-02 15:04 MST")
	result := systemDiagnostics{
		WhatsApp:   s.checkWhatsApp(ctx, now),
		OpenRouter: s.checkOpenRouter(ctx, now),
		Webhook:    s.checkPublicWebhook(ctx, now),
	}
	s.diagnostics.set(result)
	if s.logger != nil {
		s.logger.Info("integration diagnostics completed", "whatsapp", result.WhatsApp.State, "openrouter", result.OpenRouter.State, "webhook", result.Webhook.State)
	}
	return result
}

func (s *Server) checkWhatsApp(ctx context.Context, checkedAt string) diagnosticStatus {
	client := s.diagnosticsClient()
	wa := s.assistant.WhatsApp
	wabaID := strings.TrimSpace(s.config.ExpectedWABAID)
	if wa == nil || strings.TrimSpace(wa.AccessToken) == "" || strings.TrimSpace(wa.PhoneNumberID) == "" || wabaID == "" {
		return invalidDiagnostic("Missing token, Phone Number ID, or WABA ID.", checkedAt)
	}
	baseURL := strings.TrimRight(wa.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://graph.facebook.com"
	}
	version := strings.Trim(wa.GraphVersion, "/")
	phoneURL := fmt.Sprintf("%s/%s/%s?fields=id", baseURL, version, url.PathEscape(wa.PhoneNumberID))
	var phone struct {
		ID string `json:"id"`
	}
	if err := diagnosticJSON(ctx, client, phoneURL, wa.AccessToken, &phone); err != nil {
		return invalidDiagnostic("Meta rejected the access token or Phone Number ID: "+diagnosticError(err), checkedAt)
	}
	if phone.ID != wa.PhoneNumberID {
		return invalidDiagnostic("Meta returned a different Phone Number ID.", checkedAt)
	}
	appsURL := fmt.Sprintf("%s/%s/%s/subscribed_apps", baseURL, version, url.PathEscape(wabaID))
	var apps struct {
		Data []struct {
			SubscribedFields []string `json:"subscribed_fields"`
		} `json:"data"`
	}
	if err := diagnosticJSON(ctx, client, appsURL, wa.AccessToken, &apps); err != nil {
		return invalidDiagnostic("Token and phone are valid, but the WABA subscription check failed: "+diagnosticError(err), checkedAt)
	}
	if len(apps.Data) == 0 {
		return invalidDiagnostic("Token and phone are valid, but no app is subscribed to this WABA.", checkedAt)
	}
	fieldsReturned := false
	messages := false
	for _, app := range apps.Data {
		if len(app.SubscribedFields) > 0 {
			fieldsReturned = true
		}
		for _, field := range app.SubscribedFields {
			if field == "messages" {
				messages = true
			}
		}
	}
	if fieldsReturned && !messages {
		return invalidDiagnostic("WABA is subscribed, but the messages webhook field is missing.", checkedAt)
	}
	detail := "Access token, Phone Number ID, and WABA app subscription are valid."
	if messages {
		detail = "Access token, Phone Number ID, WABA subscription, and messages field are valid."
	} else {
		detail += " Meta did not return field-level details."
	}
	return validDiagnostic(detail, checkedAt)
}

func (s *Server) checkOpenRouter(ctx context.Context, checkedAt string) diagnosticStatus {
	client := s.diagnosticsClient()
	ai := s.assistant.AI
	if ai == nil || strings.TrimSpace(ai.APIKey) == "" || strings.TrimSpace(ai.Model) == "" {
		return invalidDiagnostic("Missing API key or model.", checkedAt)
	}
	baseURL := strings.TrimRight(ai.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	var keyInfo map[string]any
	if err := diagnosticJSON(ctx, client, baseURL+"/key", ai.APIKey, &keyInfo); err != nil {
		return invalidDiagnostic("OpenRouter rejected the API key: "+diagnosticError(err), checkedAt)
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := diagnosticJSON(ctx, client, baseURL+"/models", ai.APIKey, &models); err != nil {
		return invalidDiagnostic("API key is valid, but the model list check failed: "+diagnosticError(err), checkedAt)
	}
	for _, model := range models.Data {
		if model.ID == ai.Model {
			return validDiagnostic("API key and configured model are valid.", checkedAt)
		}
	}
	return invalidDiagnostic("API key is valid, but the configured model is unavailable.", checkedAt)
}

func (s *Server) checkPublicWebhook(ctx context.Context, checkedAt string) diagnosticStatus {
	hostname := strings.TrimSpace(os.Getenv("CLOUDFLARE_PUBLIC_HOSTNAME"))
	if hostname == "" {
		return unavailableDiagnostic("Quick Tunnel URL is managed by cloudflared and is not available inside the app. Local webhook checks remain active.", checkedAt)
	}
	baseURL := hostname
	if !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		baseURL = "https://" + baseURL
	}
	challenge := "diagnostic-ready"
	endpoint := strings.TrimRight(baseURL, "/") + s.config.WebhookPath + "?hub.mode=subscribe&hub.verify_token=" + url.QueryEscape(s.config.VerifyToken) + "&hub.challenge=" + challenge
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return invalidDiagnostic("Public hostname is invalid.", checkedAt)
	}
	resp, err := s.diagnosticsClient().Do(req)
	if err != nil {
		return invalidDiagnostic("Public webhook is unreachable: "+diagnosticError(err), checkedAt)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(raw)) != challenge {
		return invalidDiagnostic(fmt.Sprintf("Public webhook verification returned HTTP %d.", resp.StatusCode), checkedAt)
	}
	return validDiagnostic("Public hostname and webhook verification handshake are valid.", checkedAt)
}

func (s *Server) diagnosticsClient() *http.Client {
	if s.config.DiagnosticsClient != nil {
		return s.config.DiagnosticsClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func diagnosticJSON(ctx context.Context, client *http.Client, endpoint, token string, destination any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(destination)
}

func diagnosticError(err error) string {
	if err == nil {
		return "unknown error"
	}
	text := strings.TrimSpace(err.Error())
	if len(text) > 180 {
		text = text[:180]
	}
	return text
}

func validDiagnostic(detail, checkedAt string) diagnosticStatus {
	return diagnosticStatus{State: "valid", Label: "Valid", Detail: detail, CheckedAt: checkedAt}
}

func invalidDiagnostic(detail, checkedAt string) diagnosticStatus {
	return diagnosticStatus{State: "invalid", Label: "Invalid", Detail: detail, CheckedAt: checkedAt}
}

func unavailableDiagnostic(detail, checkedAt string) diagnosticStatus {
	return diagnosticStatus{State: "unavailable", Label: "Unavailable", Detail: detail, CheckedAt: checkedAt}
}
