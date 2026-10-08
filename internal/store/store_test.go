package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/RossEcho/OpenReception/internal/domain"
)

func TestPersistenceCloneUpcomingAndAnalytics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	defaults := Defaults()
	defaults.AdminPhone = "972500000090"
	state, err := Open(path, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if state.Snapshot().Version != 2 || state.Snapshot().Settings.OwnerPhone != defaults.AdminPhone || !state.Snapshot().Settings.NotifyOwnerWhenShared || state.Snapshot().Settings.DeveloperContact == "" {
		t.Fatalf("defaults/migration not applied: %+v", state.Snapshot().Settings)
	}
	now := time.Now().Truncate(time.Minute)
	err = state.Update(func(st *domain.State) error {
		st.Customers["one"] = &domain.Customer{Phone: "one", PreferredName: "Dana"}
		st.DailyMessages[now.Format("2006-01-02")+"|one"] = 3
		st.DailyModelCalls[now.Format("2006-01-02")+"|one"] = 2
		st.DailyIntentCalls[now.Format("2006-01-02")+"|one"] = 4
		st.Appointments = []domain.Appointment{
			{ID: 1, Start: now.Add(2 * time.Hour), Status: "confirmed"},
			{ID: 2, Start: now.Add(time.Hour), Status: "confirmed"},
			{ID: 3, Start: now.Add(-2 * time.Hour), Status: "completed"},
			{ID: 4, Start: now.Add(3 * time.Hour), Status: "cancelled"},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := state.Snapshot()
	snapshot.Customers["one"].PreferredName = "Mutated clone"
	if state.Snapshot().Customers["one"].PreferredName != "Dana" {
		t.Fatal("Snapshot returned shared mutable state")
	}
	reopened, err := Open(path, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Customers["one"].PreferredName != "Dana" {
		t.Fatal("state did not persist across reopen")
	}
	upcoming := Upcoming(reopened.Snapshot(), now, 1)
	if len(upcoming) != 1 || upcoming[0].ID != 2 {
		t.Fatalf("upcoming=%+v", upcoming)
	}
	analytics := AnalyticsFor(reopened.Snapshot(), now)
	if analytics.TotalCustomers != 1 || analytics.MessagesToday != 3 || analytics.ModelCallsToday != 6 || analytics.UpcomingAppointments != 2 || analytics.CompletedAppointments != 1 || analytics.CancelledAppointments != 1 {
		t.Fatalf("analytics=%+v", analytics)
	}
}
