package rotator

import (
	"context"
	"testing"
	"time"

	"atomcam-rotctld/internal/atomcam"
)

type blockingDevice struct {
	moves   chan atomcam.Position
	release chan struct{}
}

func (d *blockingDevice) Move(ctx context.Context, pan, tilt float64, _, _ int) (atomcam.Position, error) {
	position := atomcam.Position{Pan: pan, Tilt: tilt}
	d.moves <- position
	select {
	case <-ctx.Done():
		return atomcam.Position{}, ctx.Err()
	case <-d.release:
		return position, nil
	}
}

func (d *blockingDevice) Position(context.Context) (atomcam.Position, error) {
	return atomcam.Position{Pan: 0, Tilt: 90}, nil
}

func (d *blockingDevice) Reset(context.Context) error { return nil }

type resetBlockingDevice struct {
	blockingDevice
	resetStarted chan struct{}
	resetRelease chan struct{}
}

func (d *resetBlockingDevice) Reset(ctx context.Context) error {
	close(d.resetStarted)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.resetRelease:
		return nil
	}
}

func TestWorkerKeepsOnlyNewestPendingPosition(t *testing.T) {
	device := &blockingDevice{moves: make(chan atomcam.Position, 3), release: make(chan struct{}, 3)}
	controller := New(device, Transform{PanScale: 1, TiltHorizon: 90, TiltScale: -1}, Options{
		Speed: 5, Priority: 1, MoveTimeout: time.Second, ResetTimeout: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller.Start(ctx)

	if err := controller.SetPosition(Position{Azimuth: 10, Elevation: 10}); err != nil {
		t.Fatal(err)
	}
	first := waitForMove(t, device.moves)
	if first.Pan != 10 || first.Tilt != 80 {
		t.Fatalf("first move = %+v", first)
	}
	_ = controller.SetPosition(Position{Azimuth: 20, Elevation: 20})
	_ = controller.SetPosition(Position{Azimuth: 30, Elevation: 30})
	device.release <- struct{}{}

	second := waitForMove(t, device.moves)
	if second.Pan != 30 || second.Tilt != 60 {
		t.Fatalf("second move = %+v, expected newest target", second)
	}
	device.release <- struct{}{}
}

func TestMoveThresholdUsesCircularAzimuthDistance(t *testing.T) {
	device := &blockingDevice{moves: make(chan atomcam.Position, 1), release: make(chan struct{}, 1)}
	controller := New(device, Transform{}, Options{Threshold: 3})
	if err := controller.SetPosition(Position{Azimuth: 359, Elevation: 10}); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetPosition(Position{Azimuth: 1, Elevation: 11}); err != nil {
		t.Fatal(err)
	}
	queued := <-controller.pending
	if queued.Azimuth != 359 || queued.Elevation != 10 {
		t.Fatalf("threshold should retain first target, got %+v", queued)
	}
}

func TestForcedPositionBypassesMoveThreshold(t *testing.T) {
	device := &blockingDevice{moves: make(chan atomcam.Position, 1), release: make(chan struct{}, 1)}
	controller := New(device, Transform{}, Options{Threshold: 5})
	if err := controller.SetPosition(Position{Azimuth: 10, Elevation: 86}); err != nil {
		t.Fatal(err)
	}
	if err := controller.SetPositionForced(Position{Azimuth: 10, Elevation: 90}); err != nil {
		t.Fatal(err)
	}
	first := <-controller.pending
	if first.Elevation != 90 {
		t.Fatalf("forced target elevation = %v, want 90", first.Elevation)
	}
}

func TestStatusRefreshesActualPositionFromDevice(t *testing.T) {
	device := &blockingDevice{moves: make(chan atomcam.Position, 1), release: make(chan struct{}, 1)}
	controller := New(device, Transform{PanOffset: 180, PanScale: 1, TiltHorizon: 0, TiltScale: 1}, Options{})

	status := controller.Status()
	if !status.ActualOK {
		t.Fatal("status actual position was not refreshed")
	}
	if status.Actual.Azimuth != 180 || status.Actual.Elevation != 90 {
		t.Fatalf("status actual = %+v, want az=180 el=90", status.Actual)
	}
}

func TestResetMovesToConfiguredRawCameraPosition(t *testing.T) {
	device := &blockingDevice{moves: make(chan atomcam.Position, 1), release: make(chan struct{}, 1)}
	resetPosition := CameraPosition{Pan: 42, Tilt: 180}
	controller := New(device, Transform{PanScale: 1, TiltHorizon: 90, TiltScale: -1}, Options{
		Speed: 5, Priority: 1, ResetTimeout: time.Second, ResetCameraPosition: &resetPosition,
	})
	result := make(chan error, 1)
	go func() { result <- controller.Reset() }()
	move := waitForMove(t, device.moves)
	if move.Pan != 42 || move.Tilt != 180 {
		t.Fatalf("post-reset move = %+v", move)
	}
	device.release <- struct{}{}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestSessionResetSkipsPostResetMoveAndUsesNewestTarget(t *testing.T) {
	device := &resetBlockingDevice{
		blockingDevice: blockingDevice{moves: make(chan atomcam.Position, 1), release: make(chan struct{}, 1)},
		resetStarted:   make(chan struct{}),
		resetRelease:   make(chan struct{}),
	}
	resetPosition := CameraPosition{Pan: 180, Tilt: 90}
	controller := New(device, Transform{PanOffset: 180, PanScale: 1, TiltHorizon: 0, TiltScale: 1}, Options{
		Speed: 5, Priority: 1, MoveTimeout: time.Second, ResetTimeout: time.Second, ResetCameraPosition: &resetPosition,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller.Start(ctx)

	result := make(chan error, 1)
	go func() { result <- controller.SessionReset() }()
	<-device.resetStarted
	if err := controller.SetPosition(Position{Azimuth: 45, Elevation: 20}); err != nil {
		t.Fatal(err)
	}
	close(device.resetRelease)

	move := waitForMove(t, device.moves)
	if move.Pan != 225 || move.Tilt != 20 {
		t.Fatalf("session reset move = %+v, want satellite target", move)
	}
	device.release <- struct{}{}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func waitForMove(t *testing.T, moves <-chan atomcam.Position) atomcam.Position {
	t.Helper()
	select {
	case move := <-moves:
		return move
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for camera move")
		return atomcam.Position{}
	}
}
