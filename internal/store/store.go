package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/RossEcho/OpenReception/internal/domain"
)

type Store struct {
	mu    sync.RWMutex
	path  string
	state domain.State
}

func Defaults() domain.Settings {
	return domain.Settings{
		BusinessName:           "Your Business",
		BusinessType:           "Appointment-based business",
		DailyModelCallLimit:    8,
		AppointmentDurationMin: 60,
		CustomerReminderHours:  24,
		AdminUpcomingNoticeMin: 60,
		NotifyOwnerWhenShared:  true,
		PanelTheme:             "light",
		PanelLanguage:          "en",
		DeveloperName:          "Ross the great",
		DeveloperContact:       "972504483162",
		DeveloperProfileURL:    "https://www.linkedin.com/in/rostislav-masyukov-728040171/",
		AssistantInstructions:  "Reply warmly and concisely in the customer's language. Never invent prices, availability, or policies.",
	}
}

func Open(path string, defaults domain.Settings) (*Store, error) {
	if path == "" {
		return nil, errors.New("state path is required")
	}
	s := &Store{path: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, fmt.Errorf("decode state: %w", err)
		}
	} else {
		s.state.Settings = defaults
	}
	s.normalize(defaults)
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) normalize(defaults domain.Settings) {
	st := &s.state
	previousVersion := st.Version
	st.Version = 2
	if st.Customers == nil {
		st.Customers = map[string]*domain.Customer{}
	}
	if st.Conversations == nil {
		st.Conversations = map[string]*domain.Conversation{}
	}
	if st.DailyModelCalls == nil {
		st.DailyModelCalls = map[string]int{}
	}
	if st.DailyIntentCalls == nil {
		st.DailyIntentCalls = map[string]int{}
	}
	if st.DailyMessages == nil {
		st.DailyMessages = map[string]int{}
	}
	if st.ProcessedMessageIDs == nil {
		st.ProcessedMessageIDs = map[string]time.Time{}
	}
	if st.AssetDeliveries == nil {
		st.AssetDeliveries = map[string]map[string]time.Time{}
	}
	if st.NextAppointmentID < 1 {
		st.NextAppointmentID = 1
	}
	if st.NextCampaignID < 1 {
		st.NextCampaignID = 1
	}
	if st.NextLotteryID < 1 {
		st.NextLotteryID = 1
	}
	for _, appointment := range st.Appointments {
		if appointment.ID >= st.NextAppointmentID {
			st.NextAppointmentID = appointment.ID + 1
		}
	}
	for _, campaign := range st.EngagementCampaigns {
		if campaign.ID >= st.NextCampaignID {
			st.NextCampaignID = campaign.ID + 1
		}
	}
	for _, draw := range st.LotteryDraws {
		if draw.ID >= st.NextLotteryID {
			st.NextLotteryID = draw.ID + 1
		}
	}
	if st.Settings.BusinessName == "" {
		st.Settings.BusinessName = defaults.BusinessName
	}
	if st.Settings.BusinessType == "" {
		st.Settings.BusinessType = defaults.BusinessType
	}
	if st.Settings.DailyModelCallLimit < 1 {
		st.Settings.DailyModelCallLimit = defaults.DailyModelCallLimit
	}
	if st.Settings.AppointmentDurationMin < 1 {
		st.Settings.AppointmentDurationMin = defaults.AppointmentDurationMin
	}
	if st.Settings.CustomerReminderHours < 1 {
		st.Settings.CustomerReminderHours = defaults.CustomerReminderHours
	}
	if st.Settings.AdminUpcomingNoticeMin < 1 {
		st.Settings.AdminUpcomingNoticeMin = defaults.AdminUpcomingNoticeMin
	}
	if st.Settings.AssistantInstructions == "" {
		st.Settings.AssistantInstructions = defaults.AssistantInstructions
	}
	if st.Settings.DeveloperName == "" {
		st.Settings.DeveloperName = defaults.DeveloperName
	}
	if st.Settings.DeveloperContact == "" {
		st.Settings.DeveloperContact = defaults.DeveloperContact
	}
	if st.Settings.DeveloperProfileURL == "" {
		st.Settings.DeveloperProfileURL = defaults.DeveloperProfileURL
	}
	if st.Settings.PanelTheme != "dark" {
		st.Settings.PanelTheme = "light"
	}
	if st.Settings.PanelLanguage != "he" {
		st.Settings.PanelLanguage = "en"
	}
	if previousVersion < 2 {
		if st.Settings.OwnerPhone == "" {
			st.Settings.OwnerPhone = st.Settings.AdminPhone
			if st.Settings.OwnerPhone == "" {
				st.Settings.OwnerPhone = defaults.OwnerPhone
			}
		}
		st.Settings.NotifyOwnerWhenShared = true
	}
}

func (s *Store) View(fn func(domain.State)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(clone(s.state))
}

func (s *Store) Update(fn func(*domain.State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.state); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *Store) Snapshot() domain.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.state)
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		// Windows does not replace an existing destination with os.Rename.
		if removeErr := os.Remove(s.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace state: %w", err)
		}
		if retryErr := os.Rename(tmp, s.path); retryErr != nil {
			return fmt.Errorf("replace state: %w", retryErr)
		}
	}
	return nil
}

func clone(in domain.State) domain.State {
	data, _ := json.Marshal(in)
	var out domain.State
	_ = json.Unmarshal(data, &out)
	return out
}

func Upcoming(st domain.State, now time.Time, limit int) []domain.Appointment {
	items := make([]domain.Appointment, 0)
	for _, a := range st.Appointments {
		if (a.Status == "confirmed" || a.Status == "owner_blocked") && a.Start.After(now.Add(-time.Minute)) {
			items = append(items, a)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Start.Before(items[j].Start) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func AnalyticsFor(st domain.State, now time.Time) domain.Analytics {
	a := domain.Analytics{TotalCustomers: len(st.Customers)}
	today := now.Format("2006-01-02")
	for key, count := range st.DailyMessages {
		if len(key) >= 10 && key[:10] == today {
			a.MessagesToday += count
		}
	}
	for key, count := range st.DailyModelCalls {
		if len(key) >= 10 && key[:10] == today {
			a.ModelCallsToday += count
		}
	}
	for key, count := range st.DailyIntentCalls {
		if len(key) >= 10 && key[:10] == today {
			a.ModelCallsToday += count
		}
	}
	for _, appt := range st.Appointments {
		switch appt.Status {
		case "confirmed":
			if appt.Start.After(now) {
				a.UpcomingAppointments++
			}
		case "completed":
			a.CompletedAppointments++
		case "cancelled":
			a.CancelledAppointments++
		}
	}
	return a
}
