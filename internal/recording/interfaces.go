package recording

import (
	"context"
	"time"
)

type IMetadataDB interface {
	GetSegments(ctx context.Context, cameraID string, from, to time.Time) ([]Segment, error)
	CreateEvent(ctx context.Context, ev *Event) error
	LinkSegmentToEvent(ctx context.Context, eventID, segmentID string) error
}

type Event struct {
	ID       string    `json:"id"`
	CameraID string    `json:"camera_id"`
	EventTS  time.Time `json:"event_ts"`
	Type     string    `json:"type"`
	Data     string    `json:"data"`
}
