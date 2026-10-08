package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RossEcho/OpenReception/internal/assistant"
	"github.com/RossEcho/OpenReception/internal/domain"
	"github.com/RossEcho/OpenReception/internal/integrations"
	"github.com/RossEcho/OpenReception/internal/store"
)

func TestControlPanelLoginAndDashboard(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), store.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	_ = state.Update(func(st *domain.State) error {
		st.Settings.DeveloperName = "Hidden Developer"
		st.Settings.DeveloperContact = "hidden-contact"
		st.Settings.DeveloperProfileURL = "https://example.test/hidden"
		return nil
	})
	location, _ := time.LoadLocation("Asia/Jerusalem")
	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte("WHATSAPP_ACCESS_TOKEN=existing-secret\nOPENROUTER_API_KEY=existing-openrouter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHATSAPP_ACCESS_TOKEN", "existing-secret")
	t.Setenv("OPENROUTER_API_KEY", "existing-openrouter")
	t.Setenv("WHATSAPP_GRAPH_API_VERSION", "v26.0")
	t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "phone-id")
	t.Setenv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
	t.Setenv("OPENROUTER_MODEL", "test/model")
	server, err := New(Config{VerifyToken: "verify", WebhookPath: "/webhook/whatsapp", ControlAuth: true, ControlPassword: "correct-password", ControlSecret: "a-long-test-cookie-secret", CookieSecure: false, Location: location, EnvFile: envPath}, state, &assistant.Assistant{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	response, err := client.PostForm(httpServer.URL+"/admin/login", url.Values{"password": {"correct-password"}})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	raw, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(raw), `id="business-calendar"`) || !strings.Contains(string(raw), "Customers") || !strings.Contains(string(raw), `href="/admin/settings"`) || !strings.Contains(string(raw), `href="/admin/system-settings"`) || !strings.Contains(string(raw), "Welcome to OpenReception") {
		t.Fatalf("dashboard missing expected sections: %s", raw)
	}
	onboardingResponse, err := client.PostForm(httpServer.URL+"/admin/onboarding/complete", url.Values{"destination": {"settings"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = onboardingResponse.Body.Close()
	if !state.Snapshot().Settings.OnboardingComplete {
		t.Fatal("onboarding completion was not persisted")
	}
	settingsResponse, err := client.Get(httpServer.URL + "/admin/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer settingsResponse.Body.Close()
	settingsRaw, _ := io.ReadAll(settingsResponse.Body)
	if !strings.Contains(string(settingsRaw), "Knowledge and policy") || !strings.Contains(string(settingsRaw), "Owner contact handoff") || !strings.Contains(string(settingsRaw), "Customer media") || !strings.Contains(string(settingsRaw), "How settings work") {
		t.Fatalf("settings page missing expected sections: %s", settingsRaw)
	}
	if strings.Contains(string(settingsRaw), "developerName") || strings.Contains(string(settingsRaw), "Developer contact") || strings.Contains(string(settingsRaw), "Developer public profile") || strings.Contains(string(settingsRaw), "Hidden Developer") {
		t.Fatalf("settings page exposed code-managed developer information: %s", settingsRaw)
	}
	systemResponse, err := client.Get(httpServer.URL + "/admin/system-settings")
	if err != nil {
		t.Fatal(err)
	}
	defer systemResponse.Body.Close()
	systemRaw, _ := io.ReadAll(systemResponse.Body)
	if !strings.Contains(string(systemRaw), "WhatsApp Cloud API") || !strings.Contains(string(systemRaw), "OpenRouter") || !strings.Contains(string(systemRaw), "Cloudflare") || strings.Contains(string(systemRaw), "existing-secret") || strings.Contains(string(systemRaw), "existing-openrouter") {
		t.Fatalf("system settings missing integrations or exposed a secret: %s", systemRaw)
	}
	systemSave, err := client.PostForm(httpServer.URL+"/admin/system-settings", url.Values{
		"panelTheme": {"dark"}, "panelLanguage": {"he"}, "whatsappWABAID": {"waba"}, "whatsappPhoneNumberID": {"phone-id"}, "whatsappGraphVersion": {"v26.0"},
		"openRouterBaseURL": {"https://openrouter.ai/api/v1"}, "openRouterModel": {"test/model"}, "openRouterFallbackModel": {"fallback/model"}, "openRouterTimeoutMS": {"60000"}, "openRouterMaxTokens": {"500"}, "openRouterTemperature": {"0.3"}, "cloudflarePublicHostname": {"whatsapp.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = systemSave.Body.Close()
	savedEnv, _ := os.ReadFile(envPath)
	if !strings.Contains(string(savedEnv), "WHATSAPP_ACCESS_TOKEN=existing-secret") || !strings.Contains(string(savedEnv), "OPENROUTER_API_KEY=existing-openrouter") || !strings.Contains(string(savedEnv), "CLOUDFLARE_PUBLIC_HOSTNAME=whatsapp.example.com") {
		t.Fatalf("system save did not preserve secrets or write fields: %s", savedEnv)
	}
	if state.Snapshot().Settings.PanelTheme != "dark" || state.Snapshot().Settings.PanelLanguage != "he" {
		t.Fatalf("panel preferences not saved: %+v", state.Snapshot().Settings)
	}
	hebrewDashboard, err := client.Get(httpServer.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	defer hebrewDashboard.Body.Close()
	hebrewRaw, _ := io.ReadAll(hebrewDashboard.Body)
	if !strings.Contains(string(hebrewRaw), `dir="rtl"`) || !strings.Contains(string(hebrewRaw), "theme-dark") || !strings.Contains(string(hebrewRaw), "הגדרות הבוט") {
		t.Fatalf("Hebrew dark panel preferences not applied: %s", hebrewRaw)
	}
	engagementResponse, err := client.Get(httpServer.URL + "/admin/engagement")
	if err != nil {
		t.Fatal(err)
	}
	defer engagementResponse.Body.Close()
	engagementRaw, _ := io.ReadAll(engagementResponse.Body)
	if engagementResponse.StatusCode != http.StatusOK || !strings.Contains(string(engagementRaw), "סקר חדש") || !strings.Contains(string(engagementRaw), "שאלון חדש") || !strings.Contains(string(engagementRaw), "הגרלה") || !strings.Contains(string(engagementRaw), "היסטוריית קמפיינים") {
		t.Fatalf("engagement page missing expected controls: status=%d body=%s", engagementResponse.StatusCode, engagementRaw)
	}
	saveResponse, err := client.PostForm(httpServer.URL+"/admin/settings", url.Values{
		"businessName": {"Updated Business"}, "dailyModelCallLimit": {"8"}, "appointmentDurationMin": {"60"}, "customerReminderHours": {"24"}, "adminUpcomingNoticeMin": {"60"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = saveResponse.Body.Close()
	saved := state.Snapshot().Settings
	if saved.DeveloperName != "Hidden Developer" || saved.DeveloperContact != "hidden-contact" || saved.DeveloperProfileURL != "https://example.test/hidden" {
		t.Fatalf("saving dashboard settings changed code-managed developer data: %+v", saved)
	}
}

func TestControlPanelDevelopmentAccessWithoutLogin(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), store.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Jerusalem")
	_ = state.Update(func(st *domain.State) error {
		st.Appointments = append(st.Appointments, domain.Appointment{ID: 7, CustomerPhone: "972500000777", CustomerName: "Calendar Customer", Service: "Brow lift", Start: time.Now().In(location).Add(time.Hour), DurationMinutes: 60, Status: "confirmed"})
		return nil
	})
	server, err := New(Config{VerifyToken: "verify", WebhookPath: "/webhook/whatsapp", ControlAuth: false, ControlSecret: "a-long-test-cookie-secret", Location: location}, state, &assistant.Assistant{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	request.Host = "127.0.0.1:3000"
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Development mode") {
		t.Fatalf("dashboard did not indicate development access")
	}
	eventsResponse := httptest.NewRecorder()
	eventsRequest := httptest.NewRequest(http.MethodGet, "/admin/calendar/events", nil)
	eventsRequest.Host = "127.0.0.1:3000"
	server.Handler().ServeHTTP(eventsResponse, eventsRequest)
	if eventsResponse.Code != http.StatusOK || !strings.Contains(eventsResponse.Body.String(), "Calendar Customer") || !strings.Contains(eventsResponse.Body.String(), "972500000777") {
		t.Fatalf("calendar event feed missing admin details: status=%d body=%s", eventsResponse.Code, eventsResponse.Body.String())
	}
	publicResponse := httptest.NewRecorder()
	publicRequest := httptest.NewRequest(http.MethodGet, "/admin", nil)
	publicRequest.Host = "example-tunnel.example"
	server.Handler().ServeHTTP(publicResponse, publicRequest)
	if publicResponse.Code != http.StatusSeeOther || publicResponse.Header().Get("Location") != "/admin/login" {
		t.Fatalf("public dashboard was not protected: status=%d location=%q", publicResponse.Code, publicResponse.Header().Get("Location"))
	}
}

func TestWebhookVerificationSignatureFilteringAndDeduplication(t *testing.T) {
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), store.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	sent := make(chan map[string]any, 4)
	whatsAppAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"messages":[{"id":"wamid.reply"}]}`)
	}))
	defer whatsAppAPI.Close()
	location, _ := time.LoadLocation("Asia/Jerusalem")
	bot := &assistant.Assistant{Store: state, WhatsApp: &integrations.WhatsApp{AccessToken: "test", GraphVersion: "v26.0", PhoneNumberID: "phone", BaseURL: whatsAppAPI.URL, Timeout: time.Second}, Location: location, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	config := Config{VerifyToken: "verify-secret", AppSecret: "app-secret", WebhookPath: "/webhook/whatsapp", ExpectedWABAID: "waba-1", ExpectedPhoneID: "phone-1", ControlSecret: "cookie-secret", Location: location}
	server, err := New(config, state, bot, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	verify := httptest.NewRecorder()
	handler.ServeHTTP(verify, httptest.NewRequest(http.MethodGet, "/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=verify-secret&hub.challenge=12345", nil))
	if verify.Code != http.StatusOK || strings.TrimSpace(verify.Body.String()) != "12345" {
		t.Fatalf("verification status=%d body=%q", verify.Code, verify.Body.String())
	}
	rejectedVerify := httptest.NewRecorder()
	handler.ServeHTTP(rejectedVerify, httptest.NewRequest(http.MethodGet, "/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=12345", nil))
	if rejectedVerify.Code != http.StatusForbidden {
		t.Fatalf("bad verification status=%d", rejectedVerify.Code)
	}
	payload := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-1","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"phone-1"},"contacts":[{"wa_id":"972500000070","profile":{"name":"Untrusted Profile"}}],"messages":[{"id":"wamid.incoming","from":"972500000070","type":"text","text":{"body":"שלום"}}]}}]}]}`)
	postWebhook := func(body []byte, signature string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/webhook/whatsapp", bytes.NewReader(body))
		request.Header.Set("X-Hub-Signature-256", signature)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	badSignature := postWebhook(payload, "sha256=00")
	if badSignature.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature status=%d", badSignature.Code)
	}
	mac := hmac.New(sha256.New, []byte(config.AppSecret))
	_, _ = mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	accepted := postWebhook(payload, signature)
	if accepted.Code != http.StatusOK || strings.TrimSpace(accepted.Body.String()) != "EVENT_RECEIVED" {
		t.Fatalf("accepted status=%d body=%q", accepted.Code, accepted.Body.String())
	}
	select {
	case body := <-sent:
		if body["to"] != "972500000070" {
			t.Fatalf("reply recipient=%v", body["to"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("accepted webhook did not reach assistant")
	}
	duplicate := postWebhook(payload, signature)
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate status=%d", duplicate.Code)
	}
	select {
	case body := <-sent:
		t.Fatalf("duplicate message generated another reply: %+v", body)
	case <-time.After(150 * time.Millisecond):
	}
	wrongWABA := bytes.Replace(payload, []byte(`"waba-1"`), []byte(`"waba-other"`), 1)
	mac = hmac.New(sha256.New, []byte(config.AppSecret))
	_, _ = mac.Write(wrongWABA)
	filtered := postWebhook(wrongWABA, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	if filtered.Code != http.StatusOK {
		t.Fatalf("filtered event status=%d", filtered.Code)
	}
	select {
	case body := <-sent:
		t.Fatalf("wrong WABA generated a reply: %+v", body)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWebhookParsesAdminImageAssetMessage(t *testing.T) {
	var payload webhookPayload
	raw := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"phone"},"contacts":[{"wa_id":"972500000002","profile":{"name":"Owner"}}],"messages":[{"id":"wamid.image","from":"972500000002","type":"image","image":{"id":"media-1","mime_type":"image/jpeg","caption":"Add as a brow result asset"}}]}}]}]}`)
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload.messages()
	if len(messages) != 1 || messages[0].Type != "image" || messages[0].MediaID != "media-1" || messages[0].MIMEType != "image/jpeg" || messages[0].Text != "Add as a brow result asset" {
		t.Fatalf("unexpected image message: %+v", messages)
	}
}
