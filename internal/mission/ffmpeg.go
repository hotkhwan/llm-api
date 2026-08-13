package mission

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// FFmpegExportQueue returns a durable queued job immediately. A bounded worker
// produces and verifies the MP4; clients poll the existing export endpoint.
type FFmpegExportQueue struct {
	Jobs     ExportJobStore
	Objects  ObjectStore
	Reader   ObjectReader
	Runner   CommandRunner
	Now      func() time.Time
	TempRoot string
	Timeout  time.Duration
}

func (q FFmpegExportQueue) EnqueueExport(ctx context.Context, request ExportRequest) (ProcessingJob, error) {
	job, _, err := q.Jobs.ClaimExport(ctx, request)
	if err != nil {
		return ProcessingJob{}, err
	}
	return job, nil
}

func (q FFmpegExportQueue) GetExport(ctx context.Context, id string) (ProcessingJob, error) {
	return q.Jobs.GetExportJob(ctx, id)
}

// Run leases persisted jobs. A restarted API reclaims an expired running
// lease, so queued exports are not orphaned by process restarts.
func (q FFmpegExportQueue) Run(ctx context.Context) {
	poll := time.NewTicker(time.Second)
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

func (q FFmpegExportQueue) runOne(ctx context.Context) bool {
	timeout := q.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	now := q.Now().UTC()
	job, request, claimed, err := q.Jobs.ClaimNextExport(ctx, now, now.Add(timeout))
	if err != nil || !claimed {
		return false
	}
	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if renderErr := q.render(jobCtx, request); renderErr != nil {
		_ = q.Jobs.FailExport(context.WithoutCancel(ctx), job.ID, boundedError(renderErr), q.Now().UTC())
		return true
	}
	_, _ = q.Jobs.CompleteExport(context.WithoutCancel(ctx), job.ID, q.Now().UTC())
	return true
}

func (q FFmpegExportQueue) render(ctx context.Context, request ExportRequest) error {
	root, err := os.MkdirTemp(q.TempRoot, "kwanni-export-")
	if err != nil {
		return fmt.Errorf("create export workspace: %w", err)
	}
	defer os.RemoveAll(root)
	assets := append([]Asset(nil), request.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].Shot < assets[j].Shot })
	if len(assets) != 3 {
		return fmt.Errorf("export requires exactly three assets")
	}
	segments := make([]string, 0, 3)
	for _, asset := range assets {
		source := filepath.Join(root, fmt.Sprintf("source-%d%s", asset.Shot, extensionFor(asset.ContentType)))
		if err := q.download(ctx, asset.StorageKey, source, asset.Bytes); err != nil {
			return err
		}
		segment := filepath.Join(root, fmt.Sprintf("segment-%d.mp4", asset.Shot))
		duration := durationForShot(request.Draft.ProductionSpec, asset.Shot)
		args := []string{"-hide_banner", "-loglevel", "error", "-y"}
		if strings.HasPrefix(asset.ContentType, "image/") {
			args = append(args, "-loop", "1", "-t", fmt.Sprint(duration))
		}
		args = append(args, "-i", source, "-t", fmt.Sprint(duration), "-vf", "scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920,format=yuv420p", "-r", "30", "-an", "-c:v", "libx264", "-movflags", "+faststart", segment)
		if _, err := q.Runner.Run(ctx, "ffmpeg", args...); err != nil {
			return err
		}
		segments = append(segments, segment)
	}
	listPath := filepath.Join(root, "concat.txt")
	list, err := os.Create(listPath)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(list)
	for _, segment := range segments {
		_, _ = fmt.Fprintf(writer, "file '%s'\n", strings.ReplaceAll(segment, "'", "'\\''"))
	}
	if err := writer.Flush(); err != nil {
		_ = list.Close()
		return err
	}
	if err := list.Close(); err != nil {
		return err
	}
	output := filepath.Join(root, "first-post.mp4")
	if _, err := q.Runner.Run(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", "-movflags", "+faststart", output); err != nil {
		return err
	}
	probe, err := q.Runner.Run(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_type,width,height", "-of", "json", output)
	if err != nil {
		return err
	}
	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if json.Unmarshal(probe, &parsed) != nil || len(parsed.Streams) != 1 || parsed.Streams[0].CodecType != "video" || parsed.Streams[0].Width != 1080 || parsed.Streams[0].Height != 1920 {
		return fmt.Errorf("ffprobe rejected exported video")
	}
	file, err := os.Open(output)
	if err != nil {
		return err
	}
	defer file.Close()
	metadata, err := q.Objects.Put(ctx, request.OutputKey, "video/mp4", file)
	if err != nil {
		return err
	}
	if metadata.Key != request.OutputKey || metadata.ContentType != "video/mp4" || metadata.Bytes <= 0 || metadata.SHA256 == "" {
		return fmt.Errorf("stored export metadata is invalid")
	}
	return nil
}

func (q FFmpegExportQueue) download(ctx context.Context, key, target string, expected int64) error {
	input, err := q.Reader.Get(ctx, key)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := io.CopyN(output, input, expected+1)
	closeErr := output.Close()
	if copyErr != nil && copyErr != io.EOF {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != expected {
		return fmt.Errorf("stored asset size mismatch")
	}
	return nil
}

func durationForShot(spec ProductionSpec, shot int) int {
	for _, value := range spec.Shots {
		if value.CaptureShot == shot && value.DurationSeconds > 0 {
			return value.DurationSeconds
		}
	}
	return 3
}

func boundedError(err error) string {
	value := err.Error()
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
