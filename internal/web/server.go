package web

import (
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RossEcho/OpenReception/internal/assistant"
	"github.com/RossEcho/OpenReception/internal/domain"
	"github.com/RossEcho/OpenReception/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Config struct {
	VerifyToken       string
	AppSecret         string
	WebhookPath       string
	ExpectedWABAID    string
	ExpectedPhoneID   string
	ControlAuth       bool
	ControlPassword   string
	ControlSecret     string
	CookieSecure      bool
	Location          *time.Location
	AssetDir          string
	EnvFile           string
	DiagnosticsClient *http.Client
}

type Server struct {
	config      Config
	store       *store.Store
	assistant   *assistant.Assistant
	logger      *slog.Logger
	templates   *template.Template
	diagnostics diagnosticsStore
}

func New(config Config, state *store.Store, bot *assistant.Assistant, logger *slog.Logger) (*Server, error) {
	funcs := template.FuncMap{
		"date":  func(t time.Time) string { return t.In(config.Location).Format("2006-01-02") },
		"clock": func(t time.Time) string { return t.In(config.Location).Format("15:04") },
		"stamp": func(t time.Time) string { return t.In(config.Location).Format("2006-01-02 15:04") },
		"initial": func(v string) string {
			r := []rune(strings.TrimSpace(v))
			if len(r) == 0 {
				return "#"
			}
			return string(r[0])
		},
		"tr": translatePanel,
	}
	t, err := template.New("panel").Funcs(funcs).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{config: config, store: state, assistant: bot, logger: logger, templates: t, diagnostics: diagnosticsStore{value: initialDiagnostics()}}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin", http.StatusFound) })
	mux.HandleFunc("GET "+s.config.WebhookPath, s.verifyWebhook)
	mux.HandleFunc("POST "+s.config.WebhookPath, s.receiveWebhook)
	mux.HandleFunc("GET /admin/login", s.loginPage)
	mux.HandleFunc("POST /admin/login", s.login)
	mux.HandleFunc("POST /admin/logout", s.requireAuth(s.logout))
	mux.HandleFunc("GET /admin", s.requireAuth(s.dashboard))
	mux.HandleFunc("POST /admin/onboarding/complete", s.requireAuth(s.completeOnboarding))
	mux.HandleFunc("GET /admin/calendar/events", s.requireAuth(s.calendarEvents))
	mux.HandleFunc("GET /admin/settings", s.requireAuth(s.settingsPage))
	mux.HandleFunc("GET /admin/system-settings", s.requireAuth(s.systemSettingsPage))
	mux.HandleFunc("POST /admin/system-settings", s.requireAuth(s.saveSystemSettings))
	mux.HandleFunc("POST /admin/system-settings/diagnostics", s.requireAuth(s.refreshDiagnostics))
	mux.HandleFunc("GET /admin/engagement", s.requireAuth(s.engagementPage))
	mux.HandleFunc("POST /admin/engagement/campaign", s.requireAuth(s.startEngagementCampaign))
	mux.HandleFunc("POST /admin/engagement/campaign/{id}/close", s.requireAuth(s.closeEngagementCampaign))
	mux.HandleFunc("POST /admin/engagement/lottery", s.requireAuth(s.drawLottery))
	mux.HandleFunc("POST /admin/settings", s.requireAuth(s.saveSettings))
	mux.HandleFunc("POST /admin/appointment", s.requireAuth(s.saveAppointment))
	mux.HandleFunc("POST /admin/assets", s.requireAuth(s.saveAsset))
	mux.HandleFunc("POST /admin/assets/{id}/delete", s.requireAuth(s.deleteAsset))
	mux.HandleFunc("GET /admin/assets/{id}/file", s.requireAuth(s.assetFile))
	mux.Handle("GET /static/", http.FileServerFS(assets))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) verifyWebhook(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("hub.mode") == "subscribe" && subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(s.config.VerifyToken)) == 1 && q.Get("hub.challenge") != "" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, q.Get("hub.challenge"))
		s.logger.Info("webhook verification succeeded")
		return
	}
	http.Error(w, "verification failed", http.StatusForbidden)
}

func (s *Server) receiveWebhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1_048_577))
	if err != nil || len(raw) > 1_048_576 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.config.AppSecret != "" && !validSignature(raw, r.Header.Get("X-Hub-Signature-256"), s.config.AppSecret) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		s.logger.Warn("webhook signature rejected")
		return
	}
	var payload webhookPayload
	if json.Unmarshal(raw, &payload) != nil || payload.Object != "whatsapp_business_account" {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	messages := payload.messages()
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "EVENT_RECEIVED")
	for _, message := range messages {
		if s.config.ExpectedWABAID != "" && message.WABAID != s.config.ExpectedWABAID {
			continue
		}
		if s.config.ExpectedPhoneID != "" && message.PhoneNumberID != s.config.ExpectedPhoneID {
			continue
		}
		msg := message
		go s.assistant.Process(contextWithoutCancel(r.Context()), msg)
	}
	if len(messages) > 0 {
		s.logger.Info("incoming WhatsApp messages accepted", "count", len(messages))
	}
}

type webhookPayload struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"`
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Contacts []struct {
					WAID    string `json:"wa_id"`
					Profile struct {
						Name string `json:"name"`
					} `json:"profile"`
				} `json:"contacts"`
				Messages []struct {
					ID   string `json:"id"`
					From string `json:"from"`
					Type string `json:"type"`
					Text struct {
						Body string `json:"body"`
					} `json:"text"`
					Image struct {
						ID       string `json:"id"`
						MIMEType string `json:"mime_type"`
						Caption  string `json:"caption"`
					} `json:"image"`
				} `json:"messages"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

func (p webhookPayload) messages() []domain.IncomingMessage {
	out := []domain.IncomingMessage{}
	for _, entry := range p.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			names := map[string]string{}
			for _, c := range change.Value.Contacts {
				names[c.WAID] = c.Profile.Name
			}
			for _, m := range change.Value.Messages {
				message := domain.IncomingMessage{ID: m.ID, From: m.From, Type: m.Type, CustomerName: names[m.From], PhoneNumberID: change.Value.Metadata.PhoneNumberID, WABAID: entry.ID}
				switch m.Type {
				case "text":
					message.Text = strings.TrimSpace(m.Text.Body)
				case "image":
					message.Text = strings.TrimSpace(m.Image.Caption)
					message.MediaID = m.Image.ID
					message.MIMEType = m.Image.MIMEType
				default:
					continue
				}
				if message.Text == "" && message.MediaID == "" {
					continue
				}
				out = append(out, message)
			}
		}
	}
	return out
}

func validSignature(body []byte, header, secret string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	received, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(received, mac.Sum(nil))
}

type dashboardData struct {
	Settings       domain.Settings
	Appointments   []domain.Appointment
	Customers      []domain.Customer
	Analytics      domain.Analytics
	Notice         string
	ControlAuth    bool
	Assets         []domain.BusinessAsset
	ActivePage     string
	Campaigns      []domain.EngagementCampaign
	LotteryDraws   []domain.LotteryDraw
	PanelTheme     string
	PanelLanguage  string
	Direction      string
	System         systemSettingsView
	Error          string
	ShowOnboarding bool
}

func (s *Server) panelData(st domain.State) dashboardData {
	language := st.Settings.PanelLanguage
	if language != "he" {
		language = "en"
	}
	theme := st.Settings.PanelTheme
	if theme != "dark" {
		theme = "light"
	}
	direction := "ltr"
	if language == "he" {
		direction = "rtl"
	}
	return dashboardData{Settings: st.Settings, PanelTheme: theme, PanelLanguage: language, Direction: direction}
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	st := s.store.Snapshot()
	customers := make([]domain.Customer, 0, len(st.Customers))
	for _, c := range st.Customers {
		customers = append(customers, *c)
	}
	sortCustomers(customers)
	data := s.panelData(st)
	data.Appointments, data.Customers, data.Analytics, data.Notice, data.ControlAuth, data.ActivePage = store.Upcoming(st, time.Now().In(s.config.Location), 100), customers, store.AnalyticsFor(st, time.Now().In(s.config.Location)), r.URL.Query().Get("notice"), !s.developmentAccess(r), "main"
	data.ShowOnboarding = !st.Settings.OnboardingComplete
	if err := s.templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		s.logger.Error("render dashboard", "error", err)
	}
}

func (s *Server) completeOnboarding(w http.ResponseWriter, r *http.Request) {
	err := s.store.Update(func(st *domain.State) error {
		st.Settings.OnboardingComplete = true
		return nil
	})
	if err != nil {
		redirectNoticeTo(w, r, "/admin", "Tutorial closed", err)
		return
	}
	destination := "/admin"
	if r.FormValue("destination") == "settings" {
		destination = "/admin/settings"
	} else if r.FormValue("destination") == "system" {
		destination = "/admin/system-settings"
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

type calendarEvent struct {
	ID            string         `json:"id"`
	Title         string         `json:"title"`
	Start         string         `json:"start"`
	End           string         `json:"end"`
	ExtendedProps map[string]any `json:"extendedProps"`
}

func (s *Server) calendarEvents(w http.ResponseWriter, _ *http.Request) {
	st := s.store.Snapshot()
	events := make([]calendarEvent, 0, len(st.Appointments))
	for _, appointment := range st.Appointments {
		if appointment.Status != "confirmed" && appointment.Status != "completed" && appointment.Status != "owner_blocked" {
			continue
		}
		start := appointment.Start.In(s.config.Location)
		duration := appointment.DurationMinutes
		if duration < 1 {
			duration = st.Settings.AppointmentDurationMin
		}
		title := strings.TrimSpace(appointment.CustomerName + " — " + appointment.Service)
		if appointment.Status == "owner_blocked" {
			title = "Owner time"
		}
		events = append(events, calendarEvent{
			ID:    strconv.FormatInt(appointment.ID, 10),
			Title: title,
			Start: start.Format(time.RFC3339),
			End:   start.Add(time.Duration(duration) * time.Minute).Format(time.RFC3339),
			ExtendedProps: map[string]any{
				"customer": appointment.CustomerName,
				"phone":    appointment.CustomerPhone,
				"service":  appointment.Service,
				"status":   appointment.Status,
				"note":     appointment.AdminNote,
				"duration": duration,
			},
		})
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	st := s.store.Snapshot()
	data := s.panelData(st)
	data.Assets, data.Notice, data.ControlAuth, data.ActivePage = st.Assets, r.URL.Query().Get("notice"), !s.developmentAccess(r), "settings"
	if err := s.templates.ExecuteTemplate(w, "settings.html", data); err != nil {
		s.logger.Error("render settings", "error", err)
	}
}

func (s *Server) systemSettingsPage(w http.ResponseWriter, r *http.Request) {
	st := s.store.Snapshot()
	data := s.panelData(st)
	data.System, data.Notice, data.ControlAuth, data.ActivePage = currentSystemSettings(), r.URL.Query().Get("notice"), !s.developmentAccess(r), "system"
	data.System.Diagnostics = s.diagnostics.get()
	if err := s.templates.ExecuteTemplate(w, "system-settings.html", data); err != nil {
		s.logger.Error("render system settings", "error", err)
	}
}

func (s *Server) refreshDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	s.runDiagnostics(ctx)
	http.Redirect(w, r, "/admin/system-settings?notice="+url.QueryEscape("Diagnostics refreshed"), http.StatusSeeOther)
}

func (s *Server) saveSystemSettings(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	theme := r.FormValue("panelTheme")
	if theme != "dark" {
		theme = "light"
	}
	language := r.FormValue("panelLanguage")
	if language != "he" {
		language = "en"
	}
	graphVersion := clean(r.FormValue("whatsappGraphVersion"), 20)
	if matched, _ := regexp.MatchString(`^v\d+\.\d+$`, graphVersion); !matched {
		redirectNoticeTo(w, r, "/admin/system-settings", "System settings saved", errors.New("WhatsApp Graph API version must look like v26.0"))
		return
	}
	openRouterBaseURL := clean(r.FormValue("openRouterBaseURL"), 500)
	parsedURL, urlErr := url.ParseRequestURI(openRouterBaseURL)
	if urlErr != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		redirectNoticeTo(w, r, "/admin/system-settings", "System settings saved", errors.New("OpenRouter base URL must be a valid HTTP or HTTPS URL"))
		return
	}
	updates := map[string]string{
		"AI_ENABLED":       strconv.FormatBool(r.FormValue("aiEnabled") == "on"),
		"WHATSAPP_WABA_ID": clean(r.FormValue("whatsappWABAID"), 80), "WHATSAPP_PHONE_NUMBER_ID": clean(r.FormValue("whatsappPhoneNumberID"), 80), "WHATSAPP_GRAPH_API_VERSION": graphVersion,
		"OPENROUTER_BASE_URL": openRouterBaseURL, "OPENROUTER_MODEL": clean(r.FormValue("openRouterModel"), 200), "OPENROUTER_FALLBACK_MODEL": clean(r.FormValue("openRouterFallbackModel"), 200), "OPENROUTER_HTTP_REFERER": clean(r.FormValue("openRouterHTTPReferer"), 500), "OPENROUTER_APP_NAME": clean(r.FormValue("openRouterAppName"), 120), "OPENROUTER_TIMEOUT_MS": clean(r.FormValue("openRouterTimeoutMS"), 20), "OPENROUTER_MAX_TOKENS": clean(r.FormValue("openRouterMaxTokens"), 20), "OPENROUTER_TEMPERATURE": clean(r.FormValue("openRouterTemperature"), 20),
		"CLOUDFLARE_PUBLIC_HOSTNAME": clean(r.FormValue("cloudflarePublicHostname"), 253),
	}
	secretFields := map[string]string{
		"WHATSAPP_ACCESS_TOKEN": r.FormValue("whatsappAccessToken"), "META_APP_SECRET": r.FormValue("metaAppSecret"), "WEBHOOK_VERIFY_TOKEN": r.FormValue("webhookVerifyToken"), "OPENROUTER_API_KEY": r.FormValue("openRouterAPIKey"), "CLOUDFLARE_TUNNEL_TOKEN": r.FormValue("cloudflareTunnelToken"),
	}
	for key, value := range secretFields {
		if strings.TrimSpace(value) != "" {
			updates[key] = value
		}
	}
	if updates["WHATSAPP_PHONE_NUMBER_ID"] == "" || updates["OPENROUTER_MODEL"] == "" {
		redirectNoticeTo(w, r, "/admin/system-settings", "System settings saved", errors.New("WhatsApp Phone Number ID and OpenRouter model are required"))
		return
	}
	if err := updateEnvironmentFile(s.config.EnvFile, updates); err != nil {
		redirectNoticeTo(w, r, "/admin/system-settings", "System settings saved", err)
		return
	}
	err := s.store.Update(func(st *domain.State) error {
		st.Settings.PanelTheme = theme
		st.Settings.PanelLanguage = language
		return nil
	})
	redirectNoticeTo(w, r, "/admin/system-settings", "System settings saved. Restart the containers to apply integration changes.", err)
}

func (s *Server) engagementPage(w http.ResponseWriter, r *http.Request) {
	st := s.store.Snapshot()
	campaigns := append([]domain.EngagementCampaign(nil), st.EngagementCampaigns...)
	draws := append([]domain.LotteryDraw(nil), st.LotteryDraws...)
	for i, j := 0, len(campaigns)-1; i < j; i, j = i+1, j-1 {
		campaigns[i], campaigns[j] = campaigns[j], campaigns[i]
	}
	for i, j := 0, len(draws)-1; i < j; i, j = i+1, j-1 {
		draws[i], draws[j] = draws[j], draws[i]
	}
	data := s.panelData(st)
	data.Assets, data.Campaigns, data.LotteryDraws, data.Notice, data.ControlAuth, data.ActivePage = st.Assets, campaigns, draws, r.URL.Query().Get("notice"), !s.developmentAccess(r), "engagement"
	if err := s.templates.ExecuteTemplate(w, "engagement.html", data); err != nil {
		s.logger.Error("render engagement", "error", err)
	}
}

func (s *Server) startEngagementCampaign(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	kind := clean(r.FormValue("kind"), 30)
	question := clean(r.FormValue("question"), 1000)
	options := []string{}
	for _, line := range strings.Split(r.FormValue("options"), "\n") {
		if option := clean(line, 200); option != "" {
			options = append(options, option)
		}
	}
	campaign, err := s.assistant.StartEngagementCampaign(r.Context(), kind, question, options, clean(r.FormValue("assetId"), 80))
	notice := "Campaign sent"
	if err == nil {
		notice = fmt.Sprintf("Campaign #%d sent to %d customer(s); %d failed", campaign.ID, campaign.SentCount, campaign.FailedCount)
	}
	redirectNoticeTo(w, r, "/admin/engagement", notice, err)
}

func (s *Server) closeEngagementCampaign(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = s.assistant.CloseEngagementCampaign(id)
	}
	redirectNoticeTo(w, r, "/admin/engagement", "Campaign closed", err)
}

func (s *Server) drawLottery(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sourceID, err := strconv.ParseInt(r.FormValue("sourceCampaignId"), 10, 64)
	if err != nil {
		redirectNoticeTo(w, r, "/admin/engagement", "Winner selected", errors.New("invalid lottery source"))
		return
	}
	draw, err := s.assistant.DrawLottery(sourceID)
	notice := "Winner selected"
	if err == nil {
		name := draw.WinnerName
		if name == "" {
			name = draw.WinnerPhone
		}
		notice = fmt.Sprintf("Winner: %s (%s), selected from %d eligible customer(s)", name, draw.WinnerPhone, draw.PoolSize)
	}
	redirectNoticeTo(w, r, "/admin/engagement", notice, err)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.authenticated(r) {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	data := s.panelData(s.store.Snapshot())
	data.Error = r.URL.Query().Get("error")
	_ = s.templates.ExecuteTemplate(w, "login.html", data)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(s.config.ControlPassword)) != 1 {
		http.Redirect(w, r, "/admin/login?error=Incorrect+password", http.StatusSeeOther)
		return
	}
	expiry := time.Now().Add(8 * time.Hour)
	http.SetCookie(w, &http.Cookie{Name: "openreception_admin", Value: s.signSession(expiry), Path: "/admin", HttpOnly: true, Secure: s.config.CookieSecure, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: 28800})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "openreception_admin", Path: "/admin", MaxAge: -1, HttpOnly: true, Secure: s.config.CookieSecure, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.developmentAccess(r) {
			next(w, r)
			return
		}
		if !s.authenticated(r) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
func (s *Server) signSession(expiry time.Time) string {
	payload := strconv.FormatInt(expiry.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(s.config.ControlSecret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "." + hex.EncodeToString(mac.Sum(nil))))
}
func (s *Server) authenticated(r *http.Request) bool {
	if s.developmentAccess(r) {
		return true
	}
	cookie, err := r.Cookie("openreception_admin")
	if err != nil {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return false
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return false
	}
	sig, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.config.ControlSecret))
	mac.Write([]byte(parts[0]))
	return hmac.Equal(sig, mac.Sum(nil))
}

func (s *Server) developmentAccess(r *http.Request) bool {
	if s.config.ControlAuth {
		return false
	}
	host := r.Host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	err := s.store.Update(func(st *domain.State) error {
		v := &st.Settings
		v.BusinessName = clean(r.FormValue("businessName"), 120)
		v.BusinessType = clean(r.FormValue("businessType"), 300)
		v.Pricing = clean(r.FormValue("pricing"), 8000)
		v.ServiceDurations = clean(r.FormValue("serviceDurations"), 8000)
		v.Products = clean(r.FormValue("products"), 8000)
		v.BusinessHours = clean(r.FormValue("businessHours"), 2000)
		v.AdditionalInfo = clean(r.FormValue("additionalInfo"), 12000)
		v.ConditionalInfo = clean(r.FormValue("conditionalInfo"), 12000)
		v.AssistantInstructions = clean(r.FormValue("assistantInstructions"), 4000)
		v.AdminPhone = assistant.NormalizePhone(r.FormValue("adminPhone"))
		v.OwnerPhone = assistant.NormalizePhone(r.FormValue("ownerPhone"))
		v.ShareOwnerContact = r.FormValue("shareOwnerContact") == "on"
		v.NotifyOwnerWhenShared = r.FormValue("notifyOwnerWhenShared") == "on"
		v.DailyModelCallLimit = positiveInt(r.FormValue("dailyModelCallLimit"), 8)
		v.AppointmentDurationMin = positiveInt(r.FormValue("appointmentDurationMin"), 60)
		v.CustomerReminderHours = positiveInt(r.FormValue("customerReminderHours"), 24)
		v.AdminUpcomingNoticeMin = positiveInt(r.FormValue("adminUpcomingNoticeMin"), 60)
		return nil
	})
	redirectNoticeTo(w, r, "/admin/settings", "Settings saved", err)
}

func (s *Server) saveAsset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", fmt.Errorf("image must be 5 MB or smaller"))
		return
	}
	name := clean(r.FormValue("name"), 120)
	whenToUse := clean(r.FormValue("whenToUse"), 1200)
	if name == "" || whenToUse == "" {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", fmt.Errorf("asset name and when-to-use instruction are required"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", fmt.Errorf("choose a JPEG or PNG image"))
		return
	}
	defer file.Close()
	if header.Size < 1 || header.Size > 5<<20 {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", fmt.Errorf("image must be 5 MB or smaller"))
		return
	}
	head := make([]byte, 512)
	n, _ := file.Read(head)
	mimeType := http.DetectContentType(head[:n])
	ext := ""
	switch mimeType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	default:
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", fmt.Errorf("only JPEG and PNG images are supported"))
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", err)
		return
	}
	idBytes := make([]byte, 8)
	if _, err := cryptorand.Read(idBytes); err != nil {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", err)
		return
	}
	id := hex.EncodeToString(idBytes)
	assetDir := s.config.AssetDir
	if assetDir == "" {
		assetDir = "/data/assets"
	}
	if err := os.MkdirAll(assetDir, 0o750); err != nil {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", err)
		return
	}
	localPath := filepath.Join(assetDir, id+ext)
	out, err := os.OpenFile(localPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", err)
		return
	}
	written, copyErr := io.Copy(out, io.LimitReader(file, (5<<20)+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil || written > 5<<20 {
		_ = os.Remove(localPath)
		if copyErr == nil {
			copyErr = closeErr
		}
		if copyErr == nil {
			copyErr = fmt.Errorf("image must be 5 MB or smaller")
		}
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", copyErr)
		return
	}
	stored, err := os.Open(localPath)
	if err != nil {
		_ = os.Remove(localPath)
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", err)
		return
	}
	mediaID, uploadErr := s.assistant.WhatsApp.UploadMedia(r.Context(), header.Filename, mimeType, stored)
	_ = stored.Close()
	if uploadErr != nil {
		_ = os.Remove(localPath)
		redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", uploadErr)
		return
	}
	now := time.Now().UTC()
	asset := domain.BusinessAsset{ID: id, Name: name, WhenToUse: whenToUse, FileName: filepath.Base(header.Filename), MIMEType: mimeType, LocalPath: localPath, MediaID: mediaID, MediaUploadedAt: now, CreatedAt: now}
	err = s.store.Update(func(st *domain.State) error {
		st.Assets = append(st.Assets, asset)
		return nil
	})
	if err != nil {
		_ = os.Remove(localPath)
		_ = s.assistant.WhatsApp.DeleteMedia(context.Background(), mediaID)
	}
	redirectNoticeTo(w, r, "/admin/settings", "Asset uploaded", err)
}

func (s *Server) deleteAsset(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var removed domain.BusinessAsset
	err := s.store.Update(func(st *domain.State) error {
		for i, asset := range st.Assets {
			if asset.ID == id {
				removed = asset
				st.Assets = append(st.Assets[:i], st.Assets[i+1:]...)
				for phone := range st.AssetDeliveries {
					delete(st.AssetDeliveries[phone], id)
				}
				return nil
			}
		}
		return fmt.Errorf("asset not found")
	})
	if err == nil {
		_ = os.Remove(removed.LocalPath)
		if deleteErr := s.assistant.WhatsApp.DeleteMedia(r.Context(), removed.MediaID); deleteErr != nil {
			s.logger.Warn("delete WhatsApp media", "asset_id", id, "error", deleteErr)
		}
	}
	redirectNoticeTo(w, r, "/admin/settings", "Asset deleted", err)
}

func (s *Server) assetFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, asset := range s.store.Snapshot().Assets {
		if asset.ID == id {
			w.Header().Set("Content-Type", asset.MIMEType)
			w.Header().Set("Cache-Control", "private, max-age=3600")
			http.ServeFile(w, r, asset.LocalPath)
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) saveAppointment(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	date, timeText, status := r.FormValue("date"), r.FormValue("time"), r.FormValue("status")
	start, parseErr := time.ParseInLocation("2006-01-02 15:04", date+" "+timeText, s.config.Location)
	if parseErr != nil {
		redirectNotice(w, r, "Invalid date or time", parseErr)
		return
	}
	err := s.store.Update(func(st *domain.State) error {
		if status == "confirmed" {
			durationMinutes := st.Settings.AppointmentDurationMin
			for _, appointment := range st.Appointments {
				if appointment.ID == id && appointment.DurationMinutes > 0 {
					durationMinutes = appointment.DurationMinutes
					break
				}
			}
			duration := time.Duration(durationMinutes) * time.Minute
			end := start.Add(duration)
			for _, other := range st.Appointments {
				if other.ID == id || (other.Status != "confirmed" && other.Status != "owner_blocked") {
					continue
				}
				otherEnd := other.Start.Add(time.Duration(other.DurationMinutes) * time.Minute)
				if start.Before(otherEnd) && end.After(other.Start) {
					return fmt.Errorf("time conflicts with appointment #%d", other.ID)
				}
			}
		}
		for i := range st.Appointments {
			if st.Appointments[i].ID == id {
				st.Appointments[i].Start = start
				switch status {
				case "confirmed", "completed", "cancelled":
					st.Appointments[i].Status = status
				}
				st.Appointments[i].UpdatedAt = time.Now().In(s.config.Location)
				st.Appointments[i].CustomerReminderSent = false
				st.Appointments[i].AdminNoticeSent = false
				return nil
			}
		}
		return fmt.Errorf("appointment not found")
	})
	redirectNotice(w, r, "Appointment updated", err)
}

func redirectNotice(w http.ResponseWriter, r *http.Request, ok string, err error) {
	redirectNoticeTo(w, r, "/admin", ok, err)
}

func redirectNoticeTo(w http.ResponseWriter, r *http.Request, path, ok string, err error) {
	notice := ok
	if err != nil {
		notice = err.Error()
	}
	http.Redirect(w, r, path+"?notice="+url.QueryEscape(notice), http.StatusSeeOther)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func clean(v string, max int) string {
	v = strings.TrimSpace(v)
	r := []rune(v)
	if len(r) > max {
		v = string(r[:max])
	}
	return v
}
func positiveInt(v string, fallback int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}
func sortCustomers(items []domain.Customer) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].LastSeenAt.After(items[j-1].LastSeenAt); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func contextWithoutCancel(context.Context) context.Context { return context.Background() }
