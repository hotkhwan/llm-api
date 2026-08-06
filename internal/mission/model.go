package mission

import "time"

type State string

const (
	StateMissionAccepted State = "missionAccepted"
	StateCaptureStarted  State = "captureStarted"
	StateAssetsUploaded  State = "assetsUploaded"
	StateDraftGenerating State = "draftGenerating"
	StateDraftReady      State = "draftReady"
	StateExported        State = "exported"
	StatePosted          State = "posted"
)

type Product struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Price       string   `json:"price,omitempty"`
	Promotion   string   `json:"promotion,omitempty"`
	Facts       []string `json:"facts,omitempty"`
}

type Shot struct {
	Number      int    `json:"number"`
	Instruction string `json:"instruction"`
}

type Asset struct {
	Shot        int    `json:"shot"`
	StorageKey  string `json:"storageKey"`
	ContentType string `json:"contentType"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
}

type Clip struct {
	Shot       int    `json:"shot"`
	StorageKey string `json:"storageKey"`
	StartMS    int    `json:"startMs"`
	EndMS      int    `json:"endMs"`
}

type Draft struct {
	Caption     string   `json:"caption"`
	CTA         string   `json:"cta"`
	Hashtags    []string `json:"hashtags"`
	Timeline    []Clip   `json:"timeline"`
	GeneratedBy string   `json:"generatedBy"`
}

type Export struct {
	StorageKey string `json:"storageKey"`
	Format     string `json:"format"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
}

type Posted struct {
	Platform string    `json:"platform"`
	PostURL  string    `json:"postUrl,omitempty"`
	At       time.Time `json:"at"`
}

type Mission struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Product   Product   `json:"product"`
	State     State     `json:"state"`
	Shots     []Shot    `json:"shots"`
	Assets    []Asset   `json:"assets,omitempty"`
	Draft     *Draft    `json:"draft,omitempty"`
	Export    *Export   `json:"export,omitempty"`
	Posted    *Posted   `json:"posted,omitempty"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type AuditEvent struct {
	MissionID string    `json:"missionId"`
	Action    string    `json:"action"`
	From      State     `json:"from,omitempty"`
	To        State     `json:"to"`
	At        time.Time `json:"at"`
}

type CostEntry struct {
	MissionID  string    `json:"missionId"`
	Operation  string    `json:"operation"`
	Provider   string    `json:"provider"`
	Units      int64     `json:"units"`
	CostMicros int64     `json:"costMicros"`
	At         time.Time `json:"at"`
}
