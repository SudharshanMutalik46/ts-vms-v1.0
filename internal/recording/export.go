package recording

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)


type ExportService struct {
	Config *Config
	Store  ArchiveIndex
}

type ExportRequest struct {
	CameraID string    `json:"camera_id"`
	FromTS   time.Time `json:"from_ts"`
	ToTS     time.Time `json:"to_ts"`
	UserID   string    `json:"user_id"`
}

func NewExportService(cfg *Config, store ArchiveIndex) *ExportService {
	return &ExportService{Config: cfg, Store: store}
}

func (e *ExportService) QueueExport(ctx context.Context, req ExportRequest) (*ExportJob, error) {
	if e == nil || e.Store == nil || !e.Store.Available() {
		return nil, ErrDBUnavailable
	}
	if req.CameraID == "" || req.FromTS.IsZero() || req.ToTS.IsZero() || !req.FromTS.Before(req.ToTS) {
		return nil, errors.New("invalid export request")
	}
	job := &ExportJob{
		CameraID: req.CameraID,
		FromTS:   req.FromTS,
		ToTS:     req.ToTS,
		State:    "QUEUED",
	}
	if err := e.Store.CreateExportJob(ctx, job, req.UserID); err != nil {
		return nil, err
	}
	go e.run(job.ID)
	return job, nil
}

func (e *ExportService) run(jobID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	job, err := e.Store.GetExportJob(ctx, jobID)
	if err != nil || job == nil {
		return
	}
	job.State = "PROCESSING"
	_ = e.Store.UpdateExportJob(ctx, job)

	segs, err := e.Store.GetSegments(ctx, job.CameraID, job.FromTS, job.ToTS)
	if err != nil {
		job.State = "FAILED"
		job.Error = err.Error()
		_ = e.Store.UpdateExportJob(ctx, job)
		return
	}
	if len(segs) == 0 {
		job.State = "FAILED"
		job.Error = "no segments found"
		_ = e.Store.UpdateExportJob(ctx, job)
		return
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].StartTS.Before(segs[j].StartTS) })

	stageDir := filepath.Join(e.Config.Global.ExportRoot, "stage_"+job.ID)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		job.State = "FAILED"
		job.Error = err.Error()
		_ = e.Store.UpdateExportJob(ctx, job)
		return
	}
	defer os.RemoveAll(stageDir)

	valid := 0
	for _, seg := range segs {
		if _, err := os.Stat(seg.Path); err == nil {
			valid++
		}
	}
	if valid == 0 {
		job.State = "FAILED"
		job.Error = "segments missing on disk"
		_ = e.Store.UpdateExportJob(ctx, job)
		return
	}

	outName := fmt.Sprintf("%s_%s_%s.mkv", sanitize(job.CameraID), job.FromTS.Format("20060102_150405"), uuid.NewString())
	outPath := filepath.Join(e.Config.Global.ExportRoot, outName)

	idx := 0
	for _, seg := range segs {
		if _, err := os.Stat(seg.Path); err != nil {
			continue
		}
		dst := filepath.Join(stageDir, fmt.Sprintf("fragment_%05d.mkv", idx))
		if err := copyOrLink(seg.Path, dst); err != nil {
			job.State = "FAILED"
			job.Error = err.Error()
			_ = e.Store.UpdateExportJob(ctx, job)
			return
		}
		idx++
	}
	if idx == 0 {
		job.State = "FAILED"
		job.Error = "no readable staged fragments"
		_ = e.Store.UpdateExportJob(ctx, job)
		return
	}

	cmd := exec.CommandContext(ctx, e.Config.Global.GstLaunchPath,
		"-e",
		"splitmuxsrc", "location="+gstPath(filepath.Join(stageDir, "fragment_*.mkv")),
		"!",
		"h265parse",
		"!",
		"matroskamux", "streamable=true",
		"!",
		"filesink", "location="+gstPath(outPath),
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		job.State = "FAILED"
		job.Error = fmt.Errorf("concat failed: %v: %s", err, string(output)).Error()
		_ = e.Store.UpdateExportJob(ctx, job)
		return
	}

	job.State = "COMPLETED"
	job.OutputPath = outPath
	job.Error = ""
	_ = e.Store.UpdateExportJob(ctx, job)
}

func copyOrLink(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func sanitize(v string) string {
	v = strings.ReplaceAll(v, "/", "_")
	v = strings.ReplaceAll(v, "\\", "_")
	v = strings.ReplaceAll(v, ":", "_")
	return v
}
