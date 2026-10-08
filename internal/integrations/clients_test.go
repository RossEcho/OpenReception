package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenRouterFallsBackOnRateLimit(t *testing.T) {
	requested := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requested = append(requested, body.Model)
		w.Header().Set("Content-Type", "application/json")
		if body.Model == "primary" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"limited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fallback reply"}}]}`))
	}))
	defer server.Close()
	client := &OpenRouter{APIKey: "key", BaseURL: server.URL, Model: "primary", FallbackModel: "fallback", Timeout: time.Second, MaxTokens: 100, Temperature: 0}
	reply, err := client.Generate(context.Background(), "system", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "fallback reply" {
		t.Fatalf("reply=%q", reply)
	}
	if len(requested) != 2 || requested[1] != "fallback" {
		t.Fatalf("requested=%v", requested)
	}
}

func TestWhatsAppUploadsAndSendsImageMedia(t *testing.T) {
	uploaded := false
	sent := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v26.0/phone/media":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			if r.FormValue("messaging_product") != "whatsapp" || r.FormValue("type") != "image/png" {
				t.Fatalf("unexpected media form: %#v", r.MultipartForm.Value)
			}
			uploaded = true
			_, _ = w.Write([]byte(`{"id":"media-123"}`))
		case "/v26.0/phone/messages":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			image, _ := body["image"].(map[string]any)
			if body["type"] != "image" || image["id"] != "media-123" {
				t.Fatalf("unexpected image message: %#v", body)
			}
			sent = true
			_, _ = w.Write([]byte(`{"messages":[{"id":"wamid.image"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &WhatsApp{AccessToken: "token", GraphVersion: "v26.0", PhoneNumberID: "phone", BaseURL: server.URL, Timeout: time.Second}
	mediaID, err := client.UploadMedia(context.Background(), "example.png", "image/png", bytes.NewBufferString("png-data"))
	if err != nil || mediaID != "media-123" {
		t.Fatalf("mediaID=%q err=%v", mediaID, err)
	}
	messageID, err := client.SendImage(context.Background(), "972500000000", mediaID, "Example", "")
	if err != nil || messageID != "wamid.image" || !uploaded || !sent {
		t.Fatalf("messageID=%q uploaded=%v sent=%v err=%v", messageID, uploaded, sent, err)
	}
}
