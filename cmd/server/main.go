package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RossEcho/OpenReception/internal/assistant"
	"github.com/RossEcho/OpenReception/internal/domain"
	"github.com/RossEcho/OpenReception/internal/integrations"
	"github.com/RossEcho/OpenReception/internal/store"
	panel "github.com/RossEcho/OpenReception/internal/web"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	location, err := time.LoadLocation(env("BUSINESS_TIMEZONE", "Asia/Jerusalem"))
	must(err)
	defaults := store.Defaults()
	if v := strings.TrimSpace(os.Getenv("ADMIN_PHONE_NUMBER")); v != "" {
		defaults.AdminPhone = assistant.NormalizePhone(v)
	}
	state, err := store.Open(env("DATA_FILE", "/data/state.json"), defaults)
	must(err)
	if len(os.Args) > 1 && os.Args[1] == "admin-set" {
		flags := flag.NewFlagSet("admin-set", flag.ExitOnError)
		phone := flags.String("phone", "", "admin phone in international format")
		pin := flags.String("pin", "", "four-digit admin PIN")
		must(flags.Parse(os.Args[2:]))
		must(assistant.ConfigureAdminPIN(state, *phone, *pin))
		logger.Info("admin phone and PIN configured", "phone", assistant.NormalizePhone(*phone))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "services-set" {
		flags := flag.NewFlagSet("services-set", flag.ExitOnError)
		pricing := flags.String("pricing", "", "customer-visible service pricing")
		durations := flags.String("durations", "", "internal service durations")
		must(flags.Parse(os.Args[2:]))
		must(state.Update(func(st *domain.State) error {
			st.Settings.Pricing = strings.TrimSpace(*pricing)
			st.Settings.ServiceDurations = strings.TrimSpace(*durations)
			return nil
		}))
		logger.Info("service pricing and internal durations configured")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-user" {
		flags := flag.NewFlagSet("reset-user", flag.ExitOnError)
		phoneArg := flags.String("phone", "", "customer phone in international format")
		must(flags.Parse(os.Args[2:]))
		phone := assistant.NormalizePhone(*phoneArg)
		if phone == "" {
			must(errors.New("customer phone is required"))
		}
		must(state.Update(func(st *domain.State) error {
			delete(st.Conversations, phone)
			delete(st.AssetDeliveries, phone)
			for key := range st.DailyModelCalls {
				if strings.HasSuffix(key, "|"+phone) {
					delete(st.DailyModelCalls, key)
				}
			}
			for key := range st.DailyIntentCalls {
				if strings.HasSuffix(key, "|"+phone) {
					delete(st.DailyIntentCalls, key)
				}
			}
			for key := range st.DailyMessages {
				if strings.HasSuffix(key, "|"+phone) {
					delete(st.DailyMessages, key)
				}
			}
			return nil
		}))
		logger.Info("customer conversation state and daily counters reset", "phone", phone)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "developer-set" {
		flags := flag.NewFlagSet("developer-set", flag.ExitOnError)
		name := flags.String("name", "", "public developer or creator name")
		contact := flags.String("contact", "", "developer contact details")
		profile := flags.String("profile", "", "public developer profile URL")
		must(flags.Parse(os.Args[2:]))
		must(state.Update(func(st *domain.State) error {
			st.Settings.DeveloperName = strings.TrimSpace(*name)
			st.Settings.DeveloperContact = strings.TrimSpace(*contact)
			st.Settings.DeveloperProfileURL = strings.TrimSpace(*profile)
			return nil
		}))
		logger.Info("developer information configured")
		return
	}

	whatsApp := &integrations.WhatsApp{AccessToken: required("WHATSAPP_ACCESS_TOKEN"), GraphVersion: env("WHATSAPP_GRAPH_API_VERSION", "v26.0"), PhoneNumberID: required("WHATSAPP_PHONE_NUMBER_ID"), Timeout: 30 * time.Second}
	openRouter := &integrations.OpenRouter{APIKey: required("OPENROUTER_API_KEY"), BaseURL: env("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"), Model: required("OPENROUTER_MODEL"), FallbackModel: os.Getenv("OPENROUTER_FALLBACK_MODEL"), HTTPReferer: os.Getenv("OPENROUTER_HTTP_REFERER"), AppName: env("OPENROUTER_APP_NAME", "OpenReception"), Timeout: durationMS("OPENROUTER_TIMEOUT_MS", 60000), MaxTokens: intEnv("OPENROUTER_MAX_TOKENS", 500), Temperature: floatEnv("OPENROUTER_TEMPERATURE", .3)}
	assetDir := env("ASSET_DIR", "/data/assets")
	bot := &assistant.Assistant{Store: state, AI: openRouter, AIEnabled: boolEnv("AI_ENABLED", false), WhatsApp: whatsApp, Location: location, AssetDir: assetDir, Logger: logger}
	controlAuth := boolEnv("CONTROL_PANEL_AUTH_ENABLED", true)
	webServer, err := panel.New(panel.Config{VerifyToken: required("WEBHOOK_VERIFY_TOKEN"), AppSecret: required("META_APP_SECRET"), WebhookPath: env("WEBHOOK_PATH", "/webhook/whatsapp"), ExpectedWABAID: os.Getenv("WHATSAPP_WABA_ID"), ExpectedPhoneID: required("WHATSAPP_PHONE_NUMBER_ID"), ControlAuth: controlAuth, ControlPassword: os.Getenv("CONTROL_PANEL_PASSWORD"), ControlSecret: required("CONTROL_PANEL_SECRET"), CookieSecure: boolEnv("CONTROL_PANEL_COOKIE_SECURE", true), Location: location, AssetDir: assetDir, EnvFile: env("ENV_FILE", "/config/.env")}, state, bot, logger)
	must(err)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	webServer.StartDiagnostics(ctx)
	go reminderLoop(ctx, state, whatsApp, location, logger)
	server := &http.Server{Addr: ":" + env("PORT", "3000"), Handler: webServer.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("OpenReception started", "address", server.Addr, "model", openRouter.Model, "timezone", location.String())
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func reminderLoop(ctx context.Context, state *store.Store, whatsApp *integrations.WhatsApp, location *time.Location, logger *slog.Logger) {
	run := func() {
		now := time.Now().In(location)
		snapshot := state.Snapshot()
		admin := assistant.NormalizePhone(snapshot.Settings.AdminPhone)
		for _, ap := range snapshot.Appointments {
			if ap.Status != "confirmed" || !ap.Start.After(now) {
				continue
			}
			until := ap.Start.Sub(now)
			if !ap.CustomerReminderSent && until <= time.Duration(snapshot.Settings.CustomerReminderHours)*time.Hour {
				text := fmt.Sprintf("Reminder: appointment #%d for %s is on %s at %s.", ap.ID, ap.Service, ap.Start.In(location).Format("2006-01-02"), ap.Start.In(location).Format("15:04"))
				if _, err := whatsApp.SendText(ctx, ap.CustomerPhone, text, ""); err == nil {
					_ = state.Update(func(st *domain.State) error {
						for i := range st.Appointments {
							if st.Appointments[i].ID == ap.ID {
								st.Appointments[i].CustomerReminderSent = true
							}
						}
						return nil
					})
				} else {
					logger.Error("customer reminder failed", "appointment_id", ap.ID, "error", err)
				}
			}
			if admin != "" && !ap.AdminNoticeSent && until <= time.Duration(snapshot.Settings.AdminUpcomingNoticeMin)*time.Minute {
				text := fmt.Sprintf("Upcoming #%d: %s for %s (%s) at %s.\nTo ask the customer to choose a new time: request reschedule appointment #%d note: <optional note>", ap.ID, ap.Service, ap.CustomerName, ap.CustomerPhone, ap.Start.In(location).Format("15:04"), ap.ID)
				if _, err := whatsApp.SendText(ctx, admin, text, ""); err == nil {
					_ = state.Update(func(st *domain.State) error {
						for i := range st.Appointments {
							if st.Appointments[i].ID == ap.ID {
								st.Appointments[i].AdminNoticeSent = true
							}
						}
						return nil
					})
				} else {
					logger.Error("admin notice failed", "appointment_id", ap.ID, "error", err)
				}
			}
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func required(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(name + " is required")
	}
	return value
}
func env(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
func intEnv(name string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil || v < 1 {
		return fallback
	}
	return v
}
func floatEnv(name string, fallback float64) float64 {
	v, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil {
		return fallback
	}
	return v
}
func boolEnv(name string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
func durationMS(name string, fallback int) time.Duration {
	return time.Duration(intEnv(name, fallback)) * time.Millisecond
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
