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
