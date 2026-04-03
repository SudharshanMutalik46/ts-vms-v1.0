package recording

import (
	"testing"
	"time"
)

func TestRecorderWorkerShouldBackfillFinalized(t *testing.T) {
	w := &RecorderWorker{}

	if !w.shouldBackfillFinalized() {
		t.Fatal("expected zero-value worker to request finalized backfill")
	}

	w.lastFinalizedScan = time.Now()
	if w.shouldBackfillFinalized() {
		t.Fatal("expected recent finalized scan to be throttled")
	}

	w.lastFinalizedScan = time.Now().Add(-2 * finalizedBackfillInterval)
	if !w.shouldBackfillFinalized() {
		t.Fatal("expected stale finalized scan to be eligible again")
	}
}
