package rotator

import (
	"math"
	"testing"
)

func TestDefaultTiltConversion(t *testing.T) {
	transform := Transform{PanOffset: 180, PanScale: 1, TiltHorizon: 0, TiltScale: 1}
	tests := []struct {
		logical Position
		camera  CameraPosition
	}{
		{Position{Azimuth: 0, Elevation: 0}, CameraPosition{Pan: 180, Tilt: 0}},
		{Position{Azimuth: 120, Elevation: 30}, CameraPosition{Pan: 300, Tilt: 30}},
		{Position{Azimuth: 250, Elevation: 90}, CameraPosition{Pan: 70, Tilt: 90}},
	}
	for _, test := range tests {
		if got := transform.ToCamera(test.logical); got != test.camera {
			t.Errorf("ToCamera(%+v) = %+v, want %+v", test.logical, got, test.camera)
		}
	}
}

func TestTransformRoundTripWithOffsets(t *testing.T) {
	transform := Transform{PanOffset: 37, PanScale: -1, TiltHorizon: 72, TiltScale: 1}
	logical := Position{Azimuth: 120, Elevation: 45}
	camera := transform.ToCamera(logical)
	got := transform.FromCamera(camera)
	if math.Abs(got.Azimuth-logical.Azimuth) > 0.001 || math.Abs(got.Elevation-logical.Elevation) > 0.001 {
		t.Fatalf("round trip = %+v, want %+v", got, logical)
	}
}

func TestProtocolPanGapUsesNearestEndpoint(t *testing.T) {
	transform := Transform{PanScale: 1, TiltHorizon: 90, TiltScale: -1}
	if got := transform.ToCamera(Position{Azimuth: 356, Elevation: 0}).Pan; got != 355 {
		t.Fatalf("pan 356 mapped to %v, want 355", got)
	}
	if got := transform.ToCamera(Position{Azimuth: 359, Elevation: 0}).Pan; got != 0 {
		t.Fatalf("pan 359 mapped to %v, want 0", got)
	}
}

func TestAngularDistanceCrossesNorth(t *testing.T) {
	if got := angularDistance(359, 1); got != 2 {
		t.Fatalf("angularDistance = %v, want 2", got)
	}
}
