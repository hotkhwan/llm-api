package mission

import "time"

type State string

const (
	StateMissionAccepted  State = "missionAccepted"
	StateCaptureStarted   State = "captureStarted"
	StateAssetsUploaded   State = "assetsUploaded"
	StateDraftGenerating  State = "draftGenerating"
	StateDraftReady       State = "draftReady"
	StateExportQueued     State = "exportQueued"
	StateExported         State = "exported"
	StatePosted           State = "posted"
	StateResultRecorded   State = "resultRecorded"
	StateNextMissionReady State = "nextMissionReady"
)

type Product struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Price       string   `json:"price,omitempty"`
	Promotion   string   `json:"promotion,omitempty"`
	Facts       []string `json:"facts,omitempty"`
}

type ConsentEvidence struct {
	PrivacyNoticeVersion string    `json:"privacyNoticeVersion"`
	AcceptedAt           time.Time `json:"acceptedAt"`
}

// ProductionSpec is the provider-neutral source of truth for an affiliate
// short. Veo, Seedance and future renderers compile from this structure; they
// must never become the source of product or continuity facts.
type ProductionSpec struct {
	SchemaVersion   string            `json:"schemaVersion"`
	ProjectType     string            `json:"projectType"`
	DurationSeconds int               `json:"durationSeconds"`
	Platform        string            `json:"platform"`
	AspectRatio     string            `json:"aspectRatio"`
	CreativeIntent  string            `json:"creativeIntent"`
	StoryBeats      []string          `json:"storyBeats"`
	Continuity      ContinuityBible   `json:"continuity"`
	Shots           []ProductionShot  `json:"shots"`
	ProviderPrompts map[string]string `json:"providerPrompts,omitempty"`
}

type ContinuityBible struct {
	Product   ProductBible   `json:"product"`
	Character CharacterBible `json:"character"`
	Wardrobe  WardrobeBible  `json:"wardrobe"`
	Makeup    MakeupBible    `json:"makeup"`
	Location  LocationBible  `json:"location"`
	Lighting  LightingBible  `json:"lighting"`
	Camera    CameraBible    `json:"camera"`
}

type ProductBible struct {
	Name             string   `json:"name"`
	ReferenceKeys    []string `json:"referenceKeys"`
	VerifiedFacts    []string `json:"verifiedFacts"`
	RequiredDetails  []string `json:"requiredDetails"`
	ForbiddenChanges []string `json:"forbiddenChanges"`
}
type CharacterBible struct {
	Description string   `json:"description"`
	Constraints []string `json:"constraints"`
}
type WardrobeBible struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type MakeupBible struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type LocationBible struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}
type LightingBible struct {
	ID                     string `json:"id"`
	Style                  string `json:"style"`
	ColorTemperatureKelvin int    `json:"colorTemperatureKelvin"`
}
type CameraBible struct {
	Orientation string   `json:"orientation"`
	AspectRatio string   `json:"aspectRatio"`
	Constraints []string `json:"constraints"`
}

type ProductionShot struct {
	ShotID          string         `json:"shotId"`
	CaptureShot     int            `json:"captureShot"`
	DurationSeconds int            `json:"durationSeconds"`
	ShotSize        string         `json:"shotSize"`
	CameraAngle     string         `json:"cameraAngle"`
	LensMM          int            `json:"lensMm"`
	CameraMovement  string         `json:"cameraMovement"`
	SubjectAction   string         `json:"subjectAction"`
	Composition     string         `json:"composition"`
	Lighting        ShotLighting   `json:"lighting"`
	Continuity      ShotContinuity `json:"continuity"`
}
type ShotLighting struct {
	Style                  string `json:"style"`
	KeyDirection           string `json:"keyDirection"`
	ColorTemperatureKelvin int    `json:"colorTemperatureKelvin"`
}
type ShotContinuity struct {
	ProductOrientation string `json:"productOrientation"`
	WardrobeID         string `json:"wardrobeId"`
	LocationID         string `json:"locationId"`
}

type AgentRole string

const (
	RoleCreativeDirector  AgentRole = "creativeDirector"
	RoleStoryDirector     AgentRole = "storyDirector"
	RoleBrandGuard        AgentRole = "brandGuard"
	RoleProductionPlanner AgentRole = "productionPlanner"
	RolePromptCompiler    AgentRole = "promptCompiler"
)

type RoleExecution struct {
	Role    AgentRole `json:"role"`
	Runtime string    `json:"runtime"`
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

type ProductReference struct {
	Index       int    `json:"index"`
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
	Caption        string          `json:"caption"`
	CTA            string          `json:"cta"`
	Hashtags       []string        `json:"hashtags"`
	Timeline       []Clip          `json:"timeline"`
	GeneratedBy    string          `json:"generatedBy"`
	ProductionSpec ProductionSpec  `json:"productionSpec"`
	RoleExecutions []RoleExecution `json:"roleExecutions"`
}

type Export struct {
	StorageKey  string `json:"storageKey"`
	Format      string `json:"format"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	JobID       string `json:"jobId"`
	DownloadURL string `json:"downloadUrl,omitempty"`
}

type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
)

type ProcessingJob struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	State          JobState  `json:"state"`
	Attempt        int       `json:"attempt"`
	IdempotencyKey string    `json:"idempotencyKey"`
	LastError      string    `json:"lastError,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Posted struct {
	Platform string    `json:"platform"`
	PostURL  string    `json:"postUrl,omitempty"`
	At       time.Time `json:"at"`
}

type Outcome struct {
	Views      int64     `json:"views"`
	Clicks     int64     `json:"clicks"`
	Sales      int64     `json:"sales"`
	RecordedAt time.Time `json:"recordedAt"`
}

type NextAction struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type VisualQCMetric struct {
	ShotSize         float64 `json:"shotSize"`
	Composition      float64 `json:"composition"`
	CameraAngle      float64 `json:"cameraAngle"`
	Depth            float64 `json:"depth"`
	Lighting         float64 `json:"lighting"`
	SubjectPlacement float64 `json:"subjectPlacement"`
	ProductPlacement float64 `json:"productPlacement"`
}

type VisualQCDefect struct {
	Code             string   `json:"code"`
	Severity         string   `json:"severity"`
	Message          string   `json:"message"`
	EvidenceFrameIDs []string `json:"evidenceFrameIds"`
}

type VisualQCShot struct {
	ShotID  string           `json:"shotId"`
	Metrics VisualQCMetric   `json:"metrics"`
	Defects []VisualQCDefect `json:"defects,omitempty"`
}

type EvidenceFrame struct {
	ID          string `json:"id"`
	StorageKey  string `json:"storageKey"`
	TimestampMS int    `json:"timestampMs"`
}

type VisualQCReport struct {
	Revision       int             `json:"revision"`
	ModelRevision  string          `json:"modelRevision"`
	Threshold      float64         `json:"threshold"`
	Score          float64         `json:"score"`
	Passed         bool            `json:"passed"`
	EvidenceFrames []EvidenceFrame `json:"evidenceFrames"`
	Shots          []VisualQCShot  `json:"shots"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type VisualQCOverride struct {
	Decision string    `json:"decision"`
	Reason   string    `json:"reason"`
	By       string    `json:"by"`
	At       time.Time `json:"at"`
}

type VisualQCState struct {
	Job            *ProcessingJob    `json:"job,omitempty"`
	LatestReport   *VisualQCReport   `json:"latestReport,omitempty"`
	History        []VisualQCReport  `json:"history"`
	Warning        string            `json:"warning,omitempty"`
	ManualOverride *VisualQCOverride `json:"manualOverride,omitempty"`
}

type Mission struct {
	ID                string             `json:"id"`
	UserID            string             `json:"userId"`
	Product           Product            `json:"product"`
	Consent           ConsentEvidence    `json:"consent"`
	State             State              `json:"state"`
	Shots             []Shot             `json:"shots"`
	Assets            []Asset            `json:"assets,omitempty"`
	ProductReferences []ProductReference `json:"productReferences,omitempty"`
	Draft             *Draft             `json:"draft,omitempty"`
	Export            *Export            `json:"export,omitempty"`
	Posted            *Posted            `json:"posted,omitempty"`
	ExportJob         *ProcessingJob     `json:"exportJob,omitempty"`
	Outcome           *Outcome           `json:"outcome,omitempty"`
	NextAction        *NextAction        `json:"nextAction,omitempty"`
	VisualQC          *VisualQCState     `json:"visualQc,omitempty"`
	Version           int64              `json:"version"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
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
