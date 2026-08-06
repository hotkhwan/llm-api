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
	repo     Repository
	objects  ObjectStore
	captions CaptionGenerator
	ledger   AuditSink
	now      func() time.Time
	id       IDGenerator
}

func NewService(repo Repository, objects ObjectStore, captions CaptionGenerator, ledger AuditSink, now func() time.Time, id IDGenerator) *Service {
	return &Service{repo: repo, objects: objects, captions: captions, ledger: ledger, now: now, id: id}
}

func (s *Service) Create(ctx context.Context, userID string, product Product) (Mission, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(product.Name) == "" || strings.TrimSpace(product.Description) == "" {
		return Mission{}, fmt.Errorf("userId, product name, and description are required")
	}
	now := s.now().UTC()
	m := Mission{ID: s.id(), UserID: strings.TrimSpace(userID), Product: product, State: StateMissionAccepted, Version: 1, CreatedAt: now, UpdatedAt: now,
		Shots: []Shot{{1, "ถ่ายภาพหรือคลิปก่อนใช้สินค้า"}, {2, "ถ่ายตอนกำลังใช้สินค้า"}, {3, "ถ่ายผลลัพธ์หลังใช้สินค้า"}}}
	if err := s.repo.Create(ctx, m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "mission.created", "", m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) Get(ctx context.Context, id string) (Mission, error) { return s.repo.Get(ctx, id) }

func (s *Service) Upload(ctx context.Context, id string, shot int, contentType string, body []byte) (Mission, error) {
	if shot < 1 || shot > 3 {
		return Mission{}, fmt.Errorf("shot must be 1, 2, or 3")
	}
	if len(body) == 0 {
		return Mission{}, fmt.Errorf("asset body is required")
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
	for _, asset := range m.Assets {
		if asset.Shot == shot {
			if asset.SHA256 == digest && asset.ContentType == contentType {
				return m, nil
			}
			return Mission{}, fmt.Errorf("shot %d already contains a different asset", shot)
		}
	}
	ext := extensionFor(contentType)
	key := path.Join("missions", m.ID, "capture", fmt.Sprintf("shot-%d%s", shot, ext))
	metadata, err := s.objects.Put(ctx, key, contentType, io.LimitReader(bytes.NewReader(body), int64(len(body))))
	if err != nil {
		return Mission{}, err
	}
	from := m.State
	m.Assets = append(m.Assets, Asset{Shot: shot, StorageKey: metadata.Key, ContentType: metadata.ContentType, Bytes: metadata.Bytes, SHA256: metadata.SHA256})
	if len(m.Assets) == 3 {
		m.State = StateAssetsUploaded
	} else {
		m.State = StateCaptureStarted
	}
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "asset.uploaded", from, m.State); err != nil {
		return Mission{}, err
	}
	return m, nil
}

func (s *Service) GenerateDraft(ctx context.Context, id string) (Mission, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return Mission{}, err
	}
	if m.State == StateDraftReady || m.State == StateExported || m.State == StatePosted {
		return m, nil
	}
	if m.State != StateAssetsUploaded {
		return Mission{}, ErrInvalidState
	}
	from := m.State
	m.State = StateDraftGenerating
	result, err := s.captions.Generate(ctx, CaptionRequest{Product: m.Product, Shots: m.Shots})
	if err != nil {
		return Mission{}, err
	}
	timeline := make([]Clip, 0, 3)
	assets := append([]Asset(nil), m.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].Shot < assets[j].Shot })
	for _, asset := range assets {
		timeline = append(timeline, Clip{Shot: asset.Shot, StorageKey: asset.StorageKey, StartMS: 0, EndMS: 3000})
	}
	m.Draft = &Draft{Caption: result.Caption, CTA: result.CTA, Hashtags: result.Hashtags, Timeline: timeline, GeneratedBy: result.Provider}
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
	if m.State == StateExported || m.State == StatePosted {
		return m, nil
	}
	if m.State != StateDraftReady {
		return Mission{}, ErrInvalidState
	}
	from := m.State
	m.Export = &Export{StorageKey: path.Join("missions", m.ID, "exports", "first-post.mp4"), Format: "video/mp4", Width: 1080, Height: 1920}
	m.State = StateExported
	if err := s.save(ctx, &m); err != nil {
		return Mission{}, err
	}
	if err := s.audit(ctx, m.ID, "post.exported", from, m.State); err != nil {
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
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
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
