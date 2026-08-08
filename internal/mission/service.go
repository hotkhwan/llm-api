package mission

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

type IDGenerator func() string

type Service struct {
	repo          Repository
	objects       ObjectStore
	planner       ProductionPlanner
	exports       ExportQueue
	visualQC      VisualQCQueue
	ledger        AuditSink
	now           func() time.Time
	id            IDGenerator
	maxAssetBytes int64
}

func NewService(repo Repository, objects ObjectStore, captions CaptionGenerator, ledger AuditSink, now func() time.Time, id IDGenerator) *Service {
	return NewServiceWithOptions(repo, objects, CaptionBackedPlanner{Captions: captions}, NewMemoryJobQueue(now, id), nil, ledger, now, id, 64<<20)
}

func NewServiceWithOptions(repo Repository, objects ObjectStore, planner ProductionPlanner, exports ExportQueue, visualQC VisualQCQueue, ledger AuditSink, now func() time.Time, id IDGenerator, maxAssetBytes int64) *Service {
	return &Service{repo: repo, objects: objects, planner: planner, exports: exports, visualQC: visualQC, ledger: ledger, now: now, id: id, maxAssetBytes: maxAssetBytes}
}

func (s *Service) Create(ctx context.Context, userID string, product Product) (Mission, error) {
	return s.CreateWithConsent(ctx, userID, product, "test-fixture")
}

func (s *Service) CreateWithConsent(ctx context.Context, userID string, product Product, privacyNoticeVersion string) (Mission, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(product.Name) == "" || strings.TrimSpace(product.Description) == "" {
		return Mission{}, fmt.Errorf("userId, product name, and description are required")
	}
	privacyNoticeVersion = strings.TrimSpace(privacyNoticeVersion)
	if privacyNoticeVersion == "" {
		return Mission{}, fmt.Errorf("privacy notice consent is required")
	}
	now := s.now().UTC()
	m := Mission{ID: s.id(), UserID: strings.TrimSpace(userID), Product: product, Consent: ConsentEvidence{PrivacyNoticeVersion: privacyNoticeVersion, AcceptedAt: now}, State: StateMissionAccepted, Version: 1, CreatedAt: now, UpdatedAt: now,
		Shots: []Shot{{1, "ถ่ายภาพหรือคลิปก่อนใช้สินค้า"}, {2, "ถ่ายตอนกำลังใช้สินค้า"}, {3, "ถ่ายผลลัพธ์หลังใช้สินค้า"}}}
	if err := s.repo.Create(ctx, m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "mission.created", "", m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) Get(ctx context.Context, id string) (Mission, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if isPostExportState(m.State) && m.Export != nil {
		if err := s.reconcileVisualQC(ctx, &m); err != nil {
			m.VisualQC = visualQCWarning(m.VisualQC, "Visual QC unavailable; review the exported video manually")
		}
		m.Export.DownloadURL = s.downloadURL(ctx, m.Export.StorageKey)
		return m, nil
	}
	if m.State != StateExportQueued || m.ExportJob == nil {
		return m, nil
	}
	status, err := s.exports.GetExport(ctx, m.ExportJob.ID)
	if err != nil {
		return Mission{}, err
	}
	previous := m.ExportJob.State
	m.ExportJob = &status
	if status.State == JobSucceeded {
		from := m.State
		m.State = StateExported
		s.enqueueVisualQC(ctx, &m)
		if err := s.save(ctx, &m); err != nil {
			return Mission{}, err
		}
		if err := s.audit(ctx, m.ID, "post.exported", from, m.State); err != nil {
			return Mission{}, err
		}
		m.Export.DownloadURL = s.downloadURL(ctx, m.Export.StorageKey)
	} else if status.State != previous {
		if err := s.save(ctx, &m); err != nil {
			return Mission{}, err
		}
	}
	return m, nil
}

func isPostExportState(state State) bool {
	return state == StateExported || state == StatePosted || state == StateResultRecorded || state == StateNextMissionReady
}

func (s *Service) Upload(ctx context.Context, id string, shot int, contentType string, body []byte) (Mission, error) {
	if shot < 1 || shot > 3 {
		return Mission{}, fmt.Errorf("shot must be 1, 2, or 3")
	}
	if len(body) == 0 {
		return Mission{}, fmt.Errorf("asset body is required")
	}
	if int64(len(body)) > s.maxAssetBytes {
		return Mission{}, fmt.Errorf("asset exceeds maximum upload size")
	}
	if !strings.HasPrefix(contentType, "image/") && !strings.HasPrefix(contentType, "video/") {
		return Mission{}, fmt.Errorf("content type must be image or video")
	}
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State != StateMissionAccepted && m.State != StateCaptureStarted && m.State != StateAssetsUploaded {
		return Mission{}, ErrInvalidState
	}
	detected := strings.Split(http.DetectContentType(body), ";")[0]
	if !compatibleMediaType(contentType, detected) {
		return Mission{}, fmt.Errorf("asset bytes do not match content type")
	}
	hash := sha256.Sum256(body)
	digest := hex.EncodeToString(hash[:])
	replaceAt := -1
	for index, asset := range m.Assets {
		if asset.Shot == shot {
			if asset.SHA256 == digest && asset.ContentType == contentType {
				return m, nil
			}
			replaceAt = index
			break
		}
	}
	ext := extensionFor(contentType)
	key := path.Join("missions", m.ID, "capture", fmt.Sprintf("shot-%d-%s%s", shot, digest[:16], ext))
	metadata, err := s.objects.Put(ctx, key, contentType, io.LimitReader(bytes.NewReader(body), int64(len(body))))
	if err != nil {
		return Mission{}, err
	}
	if metadata.Key != key || metadata.ContentType != contentType || metadata.Bytes != int64(len(body)) || metadata.SHA256 != digest {
		return Mission{}, fmt.Errorf("object store returned inconsistent metadata")
	}
	from := m.State
	asset := Asset{Shot: shot, StorageKey: metadata.Key, ContentType: metadata.ContentType, Bytes: metadata.Bytes, SHA256: metadata.SHA256}
	action := "asset.uploaded"
	if replaceAt >= 0 {
		m.Assets[replaceAt] = asset
		action = "asset.replaced"
	} else {
		m.Assets = append(m.Assets, asset)
	}
	if len(m.Assets) == 3 {
		m.State = StateAssetsUploaded
	} else {
		m.State = StateCaptureStarted
	}
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, action, from, m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) UploadProductReference(ctx context.Context, id string, index int, contentType string, body []byte) (Mission, error) {
	if index < 1 || index > 5 {
		return Mission{}, fmt.Errorf("product reference index must be between 1 and 5")
	}
	if len(body) == 0 || int64(len(body)) > s.maxAssetBytes {
		return Mission{}, fmt.Errorf("product reference body is required and must be within the upload limit")
	}
	if !strings.HasPrefix(contentType, "image/") {
		return Mission{}, fmt.Errorf("product reference must be an image")
	}
	detected := strings.Split(http.DetectContentType(body), ";")[0]
	if !compatibleMediaType(contentType, detected) {
		return Mission{}, fmt.Errorf("product reference bytes do not match content type")
	}
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State != StateMissionAccepted && m.State != StateCaptureStarted && m.State != StateAssetsUploaded {
		return Mission{}, ErrInvalidState
	}
	hash := sha256.Sum256(body)
	digest := hex.EncodeToString(hash[:])
	replaceAt := -1
	for position, asset := range m.ProductReferences {
		if asset.Index == index {
			if asset.SHA256 == digest && asset.ContentType == contentType {
				return m, nil
			}
			replaceAt = position
			break
		}
	}
	key := path.Join("missions", m.ID, "product", fmt.Sprintf("reference-%d-%s%s", index, digest[:16], extensionFor(contentType)))
	metadata, err := s.objects.Put(ctx, key, contentType, bytes.NewReader(body))
	if err != nil {
		return Mission{}, err
	}
	if metadata.Key != key || metadata.ContentType != contentType || metadata.Bytes != int64(len(body)) || metadata.SHA256 != digest {
		return Mission{}, fmt.Errorf("object store returned inconsistent metadata")
	}
	reference := ProductReference{Index: index, StorageKey: key, ContentType: contentType, Bytes: metadata.Bytes, SHA256: digest}
	action := "product.referenceUploaded"
	if replaceAt >= 0 {
		m.ProductReferences[replaceAt] = reference
		action = "product.referenceReplaced"
	} else {
		m.ProductReferences = append(m.ProductReferences, reference)
	}
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, action, m.State, m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) GenerateDraft(ctx context.Context, id string) (Mission, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State == StateDraftReady || m.State == StateExportQueued || m.State == StateExported || m.State == StatePosted || m.State == StateResultRecorded || m.State == StateNextMissionReady {
		return m, nil
	}
	if m.State != StateAssetsUploaded && m.State != StateDraftGenerating {
		return Mission{}, ErrInvalidState
	}
	if len(m.ProductReferences) == 0 {
		return Mission{}, fmt.Errorf("at least one product reference image is required before draft generation")
	}
	from := m.State
	if m.State == StateAssetsUploaded {
		m.State = StateDraftGenerating
		if err := s.save(ctx, &m); err != nil {
			return Mission{}, err
		}
	}
	result, err := s.planner.Plan(ctx, PlanRequest{Product: m.Product, Shots: m.Shots})
	if err != nil {
		return Mission{}, err
	}
	// Verified product facts are never model-authored. Preserve the planner's
	// creative choices but overwrite its product bible from user evidence.
	result.ProductionSpec.Continuity.Product = deterministicProductionSpec(m.Product, m.Shots).Continuity.Product
	result.ProductionSpec.Continuity.Product.ReferenceKeys = make([]string, 0, len(m.ProductReferences))
	for _, reference := range m.ProductReferences {
		result.ProductionSpec.Continuity.Product.ReferenceKeys = append(result.ProductionSpec.Continuity.Product.ReferenceKeys, reference.StorageKey)
	}
	if !safeGeneratedContent(CaptionResult{Caption: result.Caption, CTA: result.CTA, Hashtags: result.Hashtags}) {
		return Mission{}, fmt.Errorf("planner returned unsafe generated content")
	}
	if err := validateProductionSpec(result.ProductionSpec); err != nil {
		return Mission{}, err
	}
	timeline := make([]Clip, 0, 3)
	assets := append([]Asset(nil), m.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].Shot < assets[j].Shot })
	for _, asset := range assets {
		timeline = append(timeline, Clip{Shot: asset.Shot, StorageKey: asset.StorageKey, StartMS: 0, EndMS: 3000})
	}
	m.Draft = &Draft{Caption: result.Caption, CTA: result.CTA, Hashtags: result.Hashtags, Timeline: timeline, GeneratedBy: result.Provider, ProductionSpec: result.ProductionSpec, RoleExecutions: result.Roles}
	m.State = StateDraftReady
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	now := s.now().UTC()
	if err := s.ledger.RecordCost(ctx, CostEntry{MissionID: m.ID, Operation: "caption.generate", Provider: result.Provider, Units: result.Units, CostMicros: result.CostMicros, At: now}); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "draft.generated", from, m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) Export(ctx context.Context, id string) (Mission, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State == StateExported || m.State == StatePosted || m.State == StateResultRecorded || m.State == StateNextMissionReady {
		if m.Export != nil {
			m.Export.DownloadURL = s.downloadURL(ctx, m.Export.StorageKey)
		}
		return m, nil
	}
	if m.State != StateDraftReady && m.State != StateExportQueued {
		return Mission{}, ErrInvalidState
	}
	from := m.State
	outputKey := path.Join("missions", m.ID, "exports", "first-post.mp4")
	idempotencyKey := "export:" + m.ID + ":" + fmt.Sprint(m.Version)
	if m.ExportJob != nil {
		idempotencyKey = m.ExportJob.IdempotencyKey
		status, statusErr := s.exports.GetExport(ctx, m.ExportJob.ID)
		if statusErr != nil {
			return Mission{}, statusErr
		}
		m.ExportJob = &status
		if status.State == JobSucceeded {
			m.State = StateExported
			s.enqueueVisualQC(ctx, &m)
			if err := s.save(ctx, &m); err != nil {
				return Mission{}, err
			}
			if err := s.audit(ctx, m.ID, "post.exported", from, m.State); err != nil {
				return Mission{}, err
			}
			m.Export.DownloadURL = s.downloadURL(ctx, outputKey)
			return m, nil
		}
		if status.State != JobFailed {
			return m, nil
		}
	}
	job, err := s.exports.EnqueueExport(ctx, ExportRequest{MissionID: m.ID, IdempotencyKey: idempotencyKey, Assets: append([]Asset(nil), m.Assets...), Draft: *m.Draft, OutputKey: outputKey})
	if err != nil {
		return Mission{}, err
	}
	m.ExportJob = &job
	m.Export = &Export{StorageKey: outputKey, Format: "video/mp4", Width: 1080, Height: 1920, JobID: job.ID}
	m.State = StateExportQueued
	if job.State == JobSucceeded {
		m.State = StateExported
		s.enqueueVisualQC(ctx, &m)
	}
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	action := "export.queued"
	if m.State == StateExported {
		action = "post.exported"
	}
	if err := s.audit(ctx, m.ID, action, from, m.State); err != nil {
		return Mission{}, err
	}
	if m.State == StateExported {
		m.Export.DownloadURL = s.downloadURL(ctx, outputKey)
	}
	return m, nil
}

func (s *Service) EnqueueVisualQC(ctx context.Context, id string) (Mission, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State != StateExported && m.State != StatePosted && m.State != StateResultRecorded && m.State != StateNextMissionReady {
		return Mission{}, ErrInvalidState
	}
	_ = s.reconcileVisualQC(ctx, &m)
	if m.VisualQC != nil && m.VisualQC.Job != nil {
		if m.VisualQC.Job.State == JobQueued || m.VisualQC.Job.State == JobRunning {
			return m, nil
		}
		m.VisualQC.Job = nil
	}
	before := m.Version
	s.enqueueVisualQC(ctx, &m)
	if m.Version == before {
		if err := s.save(ctx, &m); err != nil {
			return Mission{}, err
		}
	}
	return m, nil
}

func (s *Service) OverrideVisualQC(ctx context.Context, id, userID, decision, reason string) (Mission, error) {
	decision = strings.TrimSpace(decision)
	reason = strings.TrimSpace(reason)
	if decision != "accept" && decision != "reject" {
		return Mission{}, fmt.Errorf("visual QC override decision must be accept or reject")
	}
	if reason == "" || len([]rune(reason)) > 500 {
		return Mission{}, fmt.Errorf("visual QC override reason is required and must not exceed 500 characters")
	}
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State != StateExported && m.State != StatePosted && m.State != StateResultRecorded && m.State != StateNextMissionReady {
		return Mission{}, ErrInvalidState
	}
	if m.VisualQC == nil {
		m.VisualQC = &VisualQCState{}
	}
	if existing := m.VisualQC.ManualOverride; existing != nil && existing.Decision == decision && existing.Reason == reason && existing.By == userID {
		return m, nil
	}
	m.VisualQC.ManualOverride = &VisualQCOverride{Decision: decision, Reason: reason, By: userID, At: s.now().UTC()}
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "visualQc.overridden", m.State, m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) enqueueVisualQC(ctx context.Context, m *Mission) {
	if m.VisualQC == nil {
		m.VisualQC = &VisualQCState{History: []VisualQCReport{}}
	}
	if s.visualQC == nil {
		m.VisualQC.Warning = "Visual QC is not configured; review the exported video manually"
		return
	}
	if m.VisualQC.Job != nil && (m.VisualQC.Job.State == JobQueued || m.VisualQC.Job.State == JobRunning || m.VisualQC.Job.State == JobSucceeded) {
		return
	}
	revision := len(m.VisualQC.History) + 1
	key := fmt.Sprintf("visual-qc:%s:%s:%d", m.ID, m.ExportJob.ID, revision)
	job, err := s.visualQC.EnqueueVisualQC(ctx, VisualQCRequest{MissionID: m.ID, IdempotencyKey: key, VideoKey: m.Export.StorageKey, Spec: m.Draft.ProductionSpec, Revision: revision})
	if err != nil {
		m.VisualQC.Warning = "Visual QC could not be queued; review the exported video manually"
		return
	}
	m.VisualQC.Job = &job
	m.VisualQC.Warning = "Visual QC is advisory and still processing; the export remains available"
}

func (s *Service) reconcileVisualQC(ctx context.Context, m *Mission) error {
	if m.VisualQC == nil || m.VisualQC.Job == nil || s.visualQC == nil {
		return nil
	}
	job, report, err := s.visualQC.GetVisualQC(ctx, m.VisualQC.Job.ID)
	if err != nil {
		return err
	}
	changed := job.State != m.VisualQC.Job.State
	m.VisualQC.Job = &job
	switch job.State {
	case JobSucceeded:
		if report != nil && (m.VisualQC.LatestReport == nil || m.VisualQC.LatestReport.Revision != report.Revision) {
			m.VisualQC.History = append(m.VisualQC.History, *report)
			m.VisualQC.LatestReport = report
			changed = true
		}
		m.VisualQC.Warning = ""
	case JobFailed:
		m.VisualQC.Warning = "Visual QC failed; review the exported video manually or retry"
	case JobQueued, JobRunning:
		m.VisualQC.Warning = "Visual QC is advisory and still processing; the export remains available"
	}
	if changed {
		return s.save(ctx, m)
	}
	return nil
}

func visualQCWarning(state *VisualQCState, warning string) *VisualQCState {
	if state == nil {
		state = &VisualQCState{History: []VisualQCReport{}}
	}
	state.Warning = warning
	return state
}

func (s *Service) downloadURL(ctx context.Context, key string) string {
	if signer, ok := s.objects.(ObjectURLSigner); ok {
		value, _ := signer.DownloadURL(ctx, key, 15*time.Minute)
		return value
	}
	return ""
}

func (s *Service) RecordOutcome(ctx context.Context, id string, views, clicks, sales int64) (Mission, error) {
	if views < 0 || clicks < 0 || sales < 0 || clicks > views || sales > clicks {
		return Mission{}, fmt.Errorf("outcome counts must satisfy views >= clicks >= sales >= 0")
	}
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State == StateNextMissionReady && m.Outcome != nil && m.Outcome.Views == views && m.Outcome.Clicks == clicks && m.Outcome.Sales == sales {
		return m, nil
	}
	if m.State != StatePosted && m.State != StateResultRecorded {
		return Mission{}, ErrInvalidState
	}
	from := m.State
	m.Outcome = &Outcome{Views: views, Clicks: clicks, Sales: sales, RecordedAt: s.now().UTC()}
	m.State = StateResultRecorded
	switch {
	case sales > 0:
		m.NextAction = &NextAction{Kind: "repeat", Title: "ลองภารกิจถัดไป", Reason: "โพสต์นี้เกิดยอดขายแล้ว ลองทำรูปแบบเดิมกับอีกมุมหนึ่ง"}
	case clicks > 0:
		m.NextAction = &NextAction{Kind: "improveCTA", Title: "ลอง CTA ใหม่", Reason: "มีคนสนใจสินค้าแล้ว ลองทำคำชวนที่ชัดขึ้น"}
	default:
		m.NextAction = &NextAction{Kind: "retryHook", Title: "ลอง Hook ใหม่", Reason: "เริ่มจากปรับสองวินาทีแรกและทดลองอีกครั้ง"}
	}
	m.State = StateNextMissionReady
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "result.recorded", from, m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) MarkPosted(ctx context.Context, id, platform, postURL string) (Mission, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State != StateExported {
		if m.State == StatePosted && m.Posted != nil && m.Posted.Platform == platform && m.Posted.PostURL == postURL {
			return m, nil
		}
		return Mission{}, ErrInvalidState
	}
	if platform == "" {
		return Mission{}, fmt.Errorf("platform is required")
	}
	if postURL != "" {
		parsed, err := url.ParseRequestURI(postURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Mission{}, fmt.Errorf("postUrl must be an http(s) URL")
		}
	}
	from := m.State
	m.Posted = &Posted{Platform: platform, PostURL: postURL, At: s.now().UTC()}
	m.State = StatePosted
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "post.marked", from, m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) save(ctx context.Context, m *Mission) error {
	expected := m.Version
	m.UpdatedAt = s.now().UTC()
	if err := s.repo.Save(ctx, *m, expected); err != nil {
		return err
	}
	m.Version = expected + 1
	return nil
}
func (s *Service) audit(ctx context.Context, id, action string, from, to State) error {
	return s.ledger.RecordAudit(ctx, AuditEvent{MissionID: id, Action: action, From: from, To: to, At: s.now().UTC()})
}
func IsConflict(err error) bool {
	return errors.Is(err, ErrVersionConflict) || errors.Is(err, ErrInvalidState)
}

func extensionFor(contentType string) string {
	switch contentType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	default:
		return ".bin"
	}
}

func compatibleMediaType(claimed, detected string) bool {
	if claimed == detected {
		return true
	}
	// Browsers commonly use these equivalent values for the same bytes.
	return (claimed == "image/jpg" && detected == "image/jpeg") ||
		(claimed == "video/quicktime" && detected == "video/mp4")
}
