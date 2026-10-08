package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/RossEcho/OpenReception/internal/assistant"
	"github.com/RossEcho/OpenReception/internal/integrations"
	"github.com/RossEcho/OpenReception/internal/store"
)

func TestIntegrationDiagnosticsValidateConfiguredServices(t *testing.T) {
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer meta-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v26.0/phone-id":
			_, _ = io.WriteString(w, `{"id":"phone-id"}`)
		case "/v26.0/waba-id/subscribed_apps":
			_, _ = io.WriteString(w, `{"data":[{"subscribed_fields":["messages"]}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer meta.Close()

	openRouter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer router-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/key":
			_, _ = io.WriteString(w, `{"data":{"label":"test"}}`)
		case "/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"test/model"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer openRouter.Close()

	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/webhook/whatsapp" || r.URL.Query().Get("hub.verify_token") != "verify" {
			http.Error(w, "bad verification", http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, r.URL.Query().Get("hub.challenge"))
	}))
	defer public.Close()
	t.Setenv("CLOUDFLARE_PUBLIC_HOSTNAME", public.URL)

	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), store.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Jerusalem")
	bot := &assistant.Assistant{
		Store:    state,
		WhatsApp: &integrations.WhatsApp{AccessToken: "meta-token", GraphVersion: "v26.0", PhoneNumberID: "phone-id", BaseURL: meta.URL},
		AI:       &integrations.OpenRouter{APIKey: "router-key", Model: "test/model", BaseURL: openRouter.URL},
		Location: location,
	}
	server, err := New(Config{VerifyToken: "verify", WebhookPath: "/webhook/whatsapp", ExpectedWABAID: "waba-id", ExpectedPhoneID: "phone-id", Location: location, DiagnosticsClient: &http.Client{Timeout: time.Second}}, state, bot, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result := server.runDiagnostics(context.Background())
	if result.WhatsApp.State != "valid" || result.OpenRouter.State != "valid" || result.Webhook.State != "valid" {
		t.Fatalf("unexpected diagnostics: %+v", result)
	}
}

func TestIntegrationDiagnosticsRejectExpiredWhatsAppToken(t *testing.T) {
	meta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "expired", http.StatusUnauthorized)
	}))
	defer meta.Close()
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), store.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Jerusalem")
	bot := &assistant.Assistant{Store: state, WhatsApp: &integrations.WhatsApp{AccessToken: "expired", GraphVersion: "v26.0", PhoneNumberID: "phone-id", BaseURL: meta.URL}, AI: &integrations.OpenRouter{}, Location: location}
	server, err := New(Config{VerifyToken: "verify", WebhookPath: "/webhook/whatsapp", ExpectedWABAID: "waba-id", ExpectedPhoneID: "phone-id", Location: location}, state, bot, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	result := server.runDiagnostics(context.Background())
	if result.WhatsApp.State != "invalid" {
		t.Fatalf("expired token was not rejected: %+v", result.WhatsApp)
	}
}
