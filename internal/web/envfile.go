package web

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var safeEnvValue = regexp.MustCompile(`^[A-Za-z0-9_./:@+\-]*$`)

var editableEnvironmentFields = map[string]bool{
	"AI_ENABLED": true, "META_APP_SECRET": true, "WEBHOOK_VERIFY_TOKEN": true,
	"WHATSAPP_WABA_ID": true, "WHATSAPP_PHONE_NUMBER_ID": true, "WHATSAPP_ACCESS_TOKEN": true, "WHATSAPP_GRAPH_API_VERSION": true,
	"OPENROUTER_API_KEY": true, "OPENROUTER_BASE_URL": true, "OPENROUTER_MODEL": true, "OPENROUTER_FALLBACK_MODEL": true, "OPENROUTER_HTTP_REFERER": true, "OPENROUTER_APP_NAME": true, "OPENROUTER_TIMEOUT_MS": true, "OPENROUTER_MAX_TOKENS": true, "OPENROUTER_TEMPERATURE": true,
	"CLOUDFLARE_TUNNEL_TOKEN": true, "CLOUDFLARE_PUBLIC_HOSTNAME": true,
}

type systemSettingsView struct {
	Diagnostics               systemDiagnostics
	AIEnabled                 bool
	WhatsAppWABAID            string
	WhatsAppPhoneNumberID     string
	WhatsAppGraphVersion      string
	WhatsAppTokenConfigured   bool
	MetaSecretConfigured      bool
	WebhookVerifyConfigured   bool
	OpenRouterKeyConfigured   bool
	OpenRouterBaseURL         string
	OpenRouterModel           string
	OpenRouterFallbackModel   string
	OpenRouterHTTPReferer     string
	OpenRouterAppName         string
	OpenRouterTimeoutMS       string
	OpenRouterMaxTokens       string
	OpenRouterTemperature     string
	CloudflareTokenConfigured bool
	CloudflarePublicHostname  string
}

func currentSystemSettings() systemSettingsView {
	return systemSettingsView{
		AIEnabled: strings.EqualFold(os.Getenv("AI_ENABLED"), "true"), WhatsAppWABAID: os.Getenv("WHATSAPP_WABA_ID"), WhatsAppPhoneNumberID: os.Getenv("WHATSAPP_PHONE_NUMBER_ID"), WhatsAppGraphVersion: os.Getenv("WHATSAPP_GRAPH_API_VERSION"), WhatsAppTokenConfigured: os.Getenv("WHATSAPP_ACCESS_TOKEN") != "", MetaSecretConfigured: os.Getenv("META_APP_SECRET") != "", WebhookVerifyConfigured: os.Getenv("WEBHOOK_VERIFY_TOKEN") != "",
		OpenRouterKeyConfigured: os.Getenv("OPENROUTER_API_KEY") != "", OpenRouterBaseURL: os.Getenv("OPENROUTER_BASE_URL"), OpenRouterModel: os.Getenv("OPENROUTER_MODEL"), OpenRouterFallbackModel: os.Getenv("OPENROUTER_FALLBACK_MODEL"), OpenRouterHTTPReferer: os.Getenv("OPENROUTER_HTTP_REFERER"), OpenRouterAppName: os.Getenv("OPENROUTER_APP_NAME"), OpenRouterTimeoutMS: os.Getenv("OPENROUTER_TIMEOUT_MS"), OpenRouterMaxTokens: os.Getenv("OPENROUTER_MAX_TOKENS"), OpenRouterTemperature: os.Getenv("OPENROUTER_TEMPERATURE"),
		CloudflareTokenConfigured: os.Getenv("CLOUDFLARE_TUNNEL_TOKEN") != "", CloudflarePublicHostname: os.Getenv("CLOUDFLARE_PUBLIC_HOSTNAME"),
	}
}

func updateEnvironmentFile(path string, updates map[string]string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("environment file editing is not configured")
	}
	for key, value := range updates {
		if !editableEnvironmentFields[key] {
			return fmt.Errorf("unsupported environment field")
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("environment values cannot contain new lines")
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read environment file: %w", err)
	}
	lines := []string{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(line, "=") {
			lines = append(lines, line)
			continue
		}
		key := strings.TrimSpace(strings.SplitN(line, "=", 2)[0])
		if value, ok := updates[key]; ok {
			lines = append(lines, key+"="+dotenvValue(value))
			seen[key] = true
		} else {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	missing := make([]string, 0)
	for key := range updates {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		lines = append(lines, key+"="+dotenvValue(updates[key]))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write environment file: %w", err)
	}
	for key, value := range updates {
		_ = os.Setenv(key, value)
	}
	return nil
}

func dotenvValue(value string) string {
	if safeEnvValue.MatchString(value) {
		return value
	}
	return strconv.Quote(value)
}
