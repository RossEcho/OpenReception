package domain

import "time"

type Settings struct {
	BusinessName           string `json:"businessName"`
	BusinessType           string `json:"businessType"`
	Pricing                string `json:"pricing"`
	ServiceDurations       string `json:"serviceDurations"`
	Products               string `json:"products"`
	AdditionalInfo         string `json:"additionalInfo"`
	ConditionalInfo        string `json:"conditionalInfo"`
	DeveloperName          string `json:"developerName"`
	DeveloperContact       string `json:"developerContact"`
	DeveloperProfileURL    string `json:"developerProfileUrl"`
	BusinessHours          string `json:"businessHours"`
	AssistantInstructions  string `json:"assistantInstructions"`
	AdminPhone             string `json:"adminPhone"`
	OwnerPhone             string `json:"ownerPhone"`
	ShareOwnerContact      bool   `json:"shareOwnerContact"`
	NotifyOwnerWhenShared  bool   `json:"notifyOwnerWhenShared"`
	DailyModelCallLimit    int    `json:"dailyModelCallLimit"`
	AppointmentDurationMin int    `json:"appointmentDurationMin"`
	CustomerReminderHours  int    `json:"customerReminderHours"`
	AdminUpcomingNoticeMin int    `json:"adminUpcomingNoticeMin"`
	PanelTheme             string `json:"panelTheme,omitempty"`
	PanelLanguage          string `json:"panelLanguage,omitempty"`
	OnboardingComplete     bool   `json:"onboardingComplete,omitempty"`
}

type Appointment struct {
	ID                   int64     `json:"id"`
	CustomerPhone        string    `json:"customerPhone"`
	CustomerName         string    `json:"customerName"`
	Service              string    `json:"service"`
	Start                time.Time `json:"start"`
	DurationMinutes      int       `json:"durationMinutes"`
	Status               string    `json:"status"`
	AdminNote            string    `json:"adminNote,omitempty"`
	RelatedAppointmentID int64     `json:"relatedAppointmentId,omitempty"`
	CustomerReminderSent bool      `json:"customerReminderSent"`
	AdminNoticeSent      bool      `json:"adminNoticeSent"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type Customer struct {
	Phone               string    `json:"phone"`
	Name                string    `json:"name"`
	PreferredName       string    `json:"preferredName,omitempty"`
	BroadcastOptIn      bool      `json:"broadcastOptIn,omitempty"`
	BroadcastOptOut     bool      `json:"broadcastOptOut,omitempty"`
	BroadcastConsentAt  time.Time `json:"broadcastConsentAskedAt,omitempty"`
	BroadcastPreference time.Time `json:"broadcastPreferenceAt,omitempty"`
	FirstSeenAt         time.Time `json:"firstSeenAt"`
	LastSeenAt          time.Time `json:"lastSeenAt"`
	LastGreetingAt      time.Time `json:"lastGreetingAt,omitempty"`
	MessageCount        int       `json:"messageCount"`
	ModelCallCount      int       `json:"modelCallCount"`
}

type BusinessAsset struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	WhenToUse       string    `json:"whenToUse"`
	FileName        string    `json:"fileName"`
	MIMEType        string    `json:"mimeType"`
	LocalPath       string    `json:"localPath"`
	MediaID         string    `json:"mediaId"`
	MediaUploadedAt time.Time `json:"mediaUploadedAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

type EngagementResponse struct {
	CustomerPhone string    `json:"customerPhone"`
	CustomerName  string    `json:"customerName"`
	Answer        string    `json:"answer"`
	OptionIndex   int       `json:"optionIndex,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type EngagementCampaign struct {
	ID          int64                `json:"id"`
	Kind        string               `json:"kind"`
	Question    string               `json:"question"`
	Options     []string             `json:"options,omitempty"`
	AssetID     string               `json:"assetId,omitempty"`
	Status      string               `json:"status"`
	SentCount   int                  `json:"sentCount"`
	FailedCount int                  `json:"failedCount"`
	Responses   []EngagementResponse `json:"responses,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
	ClosedAt    time.Time            `json:"closedAt,omitempty"`
}

type LotteryDraw struct {
	ID               int64     `json:"id"`
	SourceCampaignID int64     `json:"sourceCampaignId,omitempty"`
	WinnerPhone      string    `json:"winnerPhone"`
	WinnerName       string    `json:"winnerName"`
	PoolSize         int       `json:"poolSize"`
	CreatedAt        time.Time `json:"createdAt"`
}

type BookingDraft struct {
	Service         string `json:"service"`
	DurationMinutes int    `json:"durationMinutes"`
	Date            string `json:"date"`
	Time            string `json:"time"`
	Name            string `json:"name"`
}

// ConversationEvent is compact derived memory. It intentionally contains no
// customer or assistant message text.
type ConversationEvent struct {
	At           time.Time `json:"at"`
	Intent       string    `json:"intent"`
	Services     []string  `json:"services,omitempty"`
	Outcome      string    `json:"outcome"`
	BookingState string    `json:"bookingState,omitempty"`
}

type Conversation struct {
	State                   string              `json:"state"`
	Language                string              `json:"language,omitempty"`
	AdminMode               bool                `json:"adminMode,omitempty"`
	Draft                   BookingDraft        `json:"draft"`
	BookingExpiresAt        time.Time           `json:"bookingExpiresAt,omitempty"`
	RecentServices          []string            `json:"recentServices,omitempty"`
	RecentServicesAt        time.Time           `json:"recentServicesAt,omitempty"`
	UnrelatedStrikes        int                 `json:"unrelatedStrikes,omitempty"`
	NameRequested           bool                `json:"nameRequested,omitempty"`
	PendingCancelID         int64               `json:"pendingCancelId,omitempty"`
	RescheduleAppointmentID int64               `json:"rescheduleAppointmentId,omitempty"`
	PendingAssetIDs         []string            `json:"pendingAssetIds,omitempty"`
	AssetRequest            bool                `json:"assetRequest,omitempty"`
	Context                 []ConversationEvent `json:"context,omitempty"`
	AdminSessionUntil       time.Time           `json:"adminSessionUntil"`
	FailedPINAttempts       int                 `json:"failedPinAttempts"`
	PINLockedUntil          time.Time           `json:"pinLockedUntil"`
	PendingBroadcastMessage string              `json:"pendingBroadcastMessage,omitempty"`
	EngagementCampaignID    int64               `json:"engagementCampaignId,omitempty"`
}

type State struct {
	Version             int                             `json:"version"`
	Settings            Settings                        `json:"settings"`
	Appointments        []Appointment                   `json:"appointments"`
	Customers           map[string]*Customer            `json:"customers"`
	Conversations       map[string]*Conversation        `json:"conversations"`
	DailyModelCalls     map[string]int                  `json:"dailyModelCalls"`
	DailyIntentCalls    map[string]int                  `json:"dailyIntentCalls"`
	DailyMessages       map[string]int                  `json:"dailyMessages"`
	ProcessedMessageIDs map[string]time.Time            `json:"processedMessageIds"`
	Assets              []BusinessAsset                 `json:"assets"`
	EngagementCampaigns []EngagementCampaign            `json:"engagementCampaigns,omitempty"`
	LotteryDraws        []LotteryDraw                   `json:"lotteryDraws,omitempty"`
	AssetDeliveries     map[string]map[string]time.Time `json:"assetDeliveries"`
	AdminPINSalt        string                          `json:"adminPinSalt"`
	AdminPINHash        string                          `json:"adminPinHash"`
	NextAppointmentID   int64                           `json:"nextAppointmentId"`
	NextCampaignID      int64                           `json:"nextCampaignId"`
	NextLotteryID       int64                           `json:"nextLotteryId"`
}

type Analytics struct {
	TotalCustomers        int
	MessagesToday         int
	ModelCallsToday       int
	UpcomingAppointments  int
	CompletedAppointments int
	CancelledAppointments int
}

type IncomingMessage struct {
	ID            string
	From          string
	Text          string
	CustomerName  string
	PhoneNumberID string
	WABAID        string
	Type          string
	MediaID       string
	MIMEType      string
	FileName      string
}
