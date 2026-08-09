package mission

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	visualQCFrameCount  = 12
	maxVisualVideoBytes = 512 << 20
	maxVisualFrameBytes = 5 << 20
)

type OpenAICompatibleVisualQC struct {
	Endpoint      string
	Model         string
	ModelRevision string
	APIKey        string
	Threshold     float64
	Client        *http.Client
}

func (v OpenAICompatibleVisualQC) Analyze(ctx context.Context, spec ProductionSpec, frames []VisualQCFrame, revision int) (VisualQCReport, error) {
	endpoint, err := url.Parse(strings.TrimRight(v.Endpoint, "/") + "/chat/completions")
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return VisualQCReport{}, fmt.Errorf("invalid ShotVL endpoint")
	}
	if strings.TrimSpace(v.Model) == "" || v.Threshold < 0 || v.Threshold > 1 {
		return VisualQCReport{}, fmt.Errorf("ShotVL model and threshold are invalid")
	}
	if len(frames) == 0 || len(frames) > visualQCFrameCount {
		return VisualQCReport{}, fmt.Errorf("ShotVL requires 1-12 evidence frames")
	}
	specJSON, _ := json.Marshal(spec)
	content := []map[string]any{{"type": "text", "text": "Evaluate each shot against this canonical production spec. Return concise strict JSON keys score, passed, shots. Each shot has shotId, metrics (shotSize, composition, cameraAngle, depth, lighting, subjectPlacement, productPlacement; 0..1), defects (only the single most important defect, or an empty array; fields code, severity info|warning|critical, a message under 120 characters, evidenceFrameIds). Cite only supplied frame IDs. Do not add prose outside JSON. Production spec: " + string(specJSON)}}
	frameIDs := make(map[string]bool, len(frames))
	for _, frame := range frames {
		if len(frame.JPEG) == 0 || len(frame.JPEG) > maxVisualFrameBytes {
			return VisualQCReport{}, fmt.Errorf("invalid evidence frame size")
		}
		if frame.Evidence.ID == "" || frameIDs[frame.Evidence.ID] {
			return VisualQCReport{}, fmt.Errorf("evidence frame IDs must be unique")
		}
		frameIDs[frame.Evidence.ID] = true
		content = append(content, map[string]any{"type": "text", "text": "evidenceFrameId=" + frame.Evidence.ID}, map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(frame.JPEG)}})
	}
	payload := map[string]any{"model": v.Model, "temperature": 0.1, "max_tokens": 1536, "response_format": map[string]string{"type": "json_object"}, "messages": []map[string]any{{"role": "system", "content": "You are ShotVL, an advisory cinematic visual critic. Do not infer sales, income, or unseen product facts."}, {"role": "user", "content": content}}}
	body, _ := json.Marshal(payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return VisualQCReport{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if v.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+v.APIKey)
	}
	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return VisualQCReport{}, fmt.Errorf("ShotVL request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return VisualQCReport{}, fmt.Errorf("ShotVL status %d", response.StatusCode)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&completion); err != nil || len(completion.Choices) == 0 {
		return VisualQCReport{}, fmt.Errorf("invalid ShotVL completion")
	}
	var report VisualQCReport
	if err := json.Unmarshal([]byte(completion.Choices[0].Message.Content), &report); err != nil {
		return VisualQCReport{}, fmt.Errorf("decode ShotVL report: %w", err)
	}
	report.Revision = revision
	report.ModelRevision = strings.TrimSpace(v.ModelRevision)
	if report.ModelRevision == "" {
		report.ModelRevision = strings.TrimSpace(v.Model)
	}
	report.Threshold = v.Threshold
	report.Passed = report.Score >= report.Threshold && report.Passed
	report.EvidenceFrames = make([]EvidenceFrame, len(frames))
	for index := range frames {
		report.EvidenceFrames[index] = frames[index].Evidence
	}
	if err := validateVisualQCReport(report, spec, frameIDs); err != nil {
		return VisualQCReport{}, err
	}
	return report, nil
}

func validateVisualQCReport(report VisualQCReport, spec ProductionSpec, frameIDs map[string]bool) error {
	if report.Revision < 1 || report.ModelRevision == "" || report.Threshold < 0 || report.Threshold > 1 || report.Score < 0 || report.Score > 1 || len(report.Shots) != len(spec.Shots) {
		return fmt.Errorf("invalid visual QC report summary")
	}
	expected := make(map[string]bool, len(spec.Shots))
	for _, shot := range spec.Shots {
		expected[shot.ShotID] = true
	}
	for _, shot := range report.Shots {
		if !expected[shot.ShotID] {
			return fmt.Errorf("visual QC report contains an unknown shot")
		}
		delete(expected, shot.ShotID)
		metrics := []float64{shot.Metrics.ShotSize, shot.Metrics.Composition, shot.Metrics.CameraAngle, shot.Metrics.Depth, shot.Metrics.Lighting, shot.Metrics.SubjectPlacement, shot.Metrics.ProductPlacement}
		for _, score := range metrics {
			if score < 0 || score > 1 {
				return fmt.Errorf("visual QC metric is outside 0..1")
			}
		}
		for _, defect := range shot.Defects {
			if defect.Code == "" || defect.Message == "" || (defect.Severity != "info" && defect.Severity != "warning" && defect.Severity != "critical") {
				return fmt.Errorf("invalid visual QC defect")
			}
			for _, id := range defect.EvidenceFrameIDs {
				if !frameIDs[id] {
					return fmt.Errorf("visual QC defect cites unknown evidence")
				}
			}
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("visual QC report omitted a shot")
	}
	return nil
}

type ShotVLQueue struct {
	Jobs     VisualQCJobStore
	Objects  ObjectStore
	Reader   ObjectReader
	Runner   CommandRunner
	Analyzer VisualQCAnalyzer
	Now      func() time.Time
	WorkerID string
	TempRoot string
	Timeout  time.Duration
}

func (q *ShotVLQueue) EnqueueVisualQC(ctx context.Context, request VisualQCRequest) (ProcessingJob, error) {
	return q.Jobs.EnqueueVisualQCJob(ctx, request)
}
func (q *ShotVLQueue) GetVisualQC(ctx context.Context, id string) (ProcessingJob, *VisualQCReport, error) {
	return q.Jobs.GetVisualQCJob(ctx, id)
}

func (q *ShotVLQueue) Run(ctx context.Context) {
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	for {
		if !q.runOne(ctx) {
			select {
			case <-ctx.Done():
				return
			case <-poll.C:
			}
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (q *ShotVLQueue) runOne(ctx context.Context) bool {
	timeout := q.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	now := q.Now().UTC()
	job, request, claimed, err := q.Jobs.ClaimNextVisualQC(ctx, q.WorkerID, now, now.Add(timeout))
	if err != nil || !claimed {
		return false
	}
	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	report, analyzeErr := q.process(jobCtx, request)
	if analyzeErr != nil {
		_ = q.Jobs.FailVisualQC(context.WithoutCancel(ctx), job.ID, q.WorkerID, boundedError(analyzeErr), q.Now().UTC())
		return true
	}
	report.CreatedAt = q.Now().UTC()
	_, _ = q.Jobs.CompleteVisualQC(context.WithoutCancel(ctx), job.ID, q.WorkerID, report, report.CreatedAt)
	return true
}

func (q *ShotVLQueue) process(ctx context.Context, request VisualQCRequest) (VisualQCReport, error) {
	root, err := os.MkdirTemp(q.TempRoot, "kwanni-visual-qc-")
	if err != nil {
		return VisualQCReport{}, err
	}
	defer os.RemoveAll(root)
	video := filepath.Join(root, "export.mp4")
	input, err := q.Reader.Get(ctx, request.VideoKey)
	if err != nil {
		return VisualQCReport{}, err
	}
	output, err := os.OpenFile(video, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		_ = input.Close()
		return VisualQCReport{}, err
	}
	n, copyErr := io.Copy(output, io.LimitReader(input, maxVisualVideoBytes+1))
	closeErr := output.Close()
	_ = input.Close()
	if copyErr != nil || closeErr != nil || n == 0 || n > maxVisualVideoBytes {
		return VisualQCReport{}, fmt.Errorf("visual QC video is empty or exceeds limit")
	}
	pattern := filepath.Join(root, "frame-%02d.jpg")
	filter := fmt.Sprintf("fps=%d/%d,scale=768:-2", visualQCFrameCount, request.Spec.DurationSeconds)
	if _, err := q.Runner.Run(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", video, "-vf", filter, "-frames:v", fmt.Sprint(visualQCFrameCount), "-q:v", "3", pattern); err != nil {
		return VisualQCReport{}, err
	}
	paths, _ := filepath.Glob(filepath.Join(root, "frame-*.jpg"))
	sort.Strings(paths)
	if len(paths) != visualQCFrameCount {
		return VisualQCReport{}, fmt.Errorf("expected %d deterministic keyframes, got %d", visualQCFrameCount, len(paths))
	}
	frames := make([]VisualQCFrame, 0, len(paths))
	for index, framePath := range paths {
		value, err := os.ReadFile(framePath)
		if err != nil || len(value) == 0 || len(value) > maxVisualFrameBytes {
			return VisualQCReport{}, fmt.Errorf("invalid extracted keyframe")
		}
		id := fmt.Sprintf("frame%02d", index)
		key := fmt.Sprintf("missions/%s/visual-qc/revision-%d/%s.jpg", request.MissionID, request.Revision, id)
		metadata, err := q.Objects.Put(ctx, key, "image/jpeg", bytes.NewReader(value))
		if err != nil {
			return VisualQCReport{}, fmt.Errorf("store visual QC evidence: %w", err)
		}
		if metadata.Key != key || metadata.Bytes != int64(len(value)) || metadata.ContentType != "image/jpeg" || metadata.SHA256 == "" {
			return VisualQCReport{}, fmt.Errorf("stored visual QC evidence metadata is invalid")
		}
		frames = append(frames, VisualQCFrame{Evidence: EvidenceFrame{ID: id, StorageKey: key, TimestampMS: index * request.Spec.DurationSeconds * 1000 / visualQCFrameCount}, JPEG: value})
	}
	return q.Analyzer.Analyze(ctx, request.Spec, frames, request.Revision)
}
