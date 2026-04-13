package recording

import (
	"context"
	"log"
)

// ArchiveIndexWriter provides a dedicated interface for the RecordingArchiver to write finalized segments.
type ArchiveIndexWriter struct {
	Index ArchiveIndex
}

func NewArchiveIndexWriter(index ArchiveIndex) *ArchiveIndexWriter {
	return &ArchiveIndexWriter{Index: index}
}

func (w *ArchiveIndexWriter) WriteFinalizedSegment(ctx context.Context, seg *ArchiveSegment) error {
	if seg.Container == "" {
		seg.Container = "mkv"
	}
	seg.VideoCodec = normalizeCodec(seg.VideoCodec)
	if seg.HealthState == "" {
		seg.HealthState = "finalized"
	}

	err := w.Index.UpsertFinalizedSegment(ctx, seg)
	if err != nil {
		log.Printf("[ArchiveIndexWriter] failed to index segment %s: %v", seg.Path, err)
		return err
	}
	return nil
}
