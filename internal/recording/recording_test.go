package recording

import (
	"testing"
	"time"
)

func TestScheduler_EventTrigger(t *testing.T) {
	sched := NewScheduleEngine([]ScheduleConfig{{CameraID: "cam_test", Type: "event_triggered"}})
	if sched.ShouldRecord("cam_test") {
		t.Error("Should not record initially")
	}

	sched.TriggerEvent("cam_test", 2)
	if !sched.ShouldRecord("cam_test") {
		t.Error("Should record after trigger")
	}

	time.Sleep(3 * time.Second)
	if sched.ShouldRecord("cam_test") {
		t.Error("Should stop recording after event expires")
	}
}

func TestLicenseGate(t *testing.T) {
	gate := NewLicenseGate(1)
	if !gate.TryAcquire("cam1") {
		t.Error("Should acquire first camera")
	}
	if gate.TryAcquire("cam2") {
		t.Error("Should deny second camera exceeding quota")
	}
	gate.Release()
	if !gate.TryAcquire("cam2") {
		t.Error("Should acquire after release")
	}
}
