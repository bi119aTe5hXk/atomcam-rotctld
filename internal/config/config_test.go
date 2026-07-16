package config

import "testing"

func TestDefaultReferencePoseFacesNorth(t *testing.T) {
	t.Setenv("ATOMCAM_URL", "http://192.0.2.1")
	t.Setenv("PAN_OFFSET", "")
	t.Setenv("RESET_CAMERA_PAN", "")
	t.Setenv("RESET_CAMERA_TILT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PanOffset != 180 {
		t.Fatalf("PanOffset = %v, want 180", cfg.PanOffset)
	}
	if cfg.TiltHorizon != 0 {
		t.Fatalf("TiltHorizon = %v, want 0", cfg.TiltHorizon)
	}
	if cfg.TiltScale != 1 {
		t.Fatalf("TiltScale = %v, want 1", cfg.TiltScale)
	}
	if cfg.ResetCameraPan != 180 || cfg.ResetCameraTilt != 90 {
		t.Fatalf("reset camera position = pan %v tilt %v, want pan 180 tilt 90", cfg.ResetCameraPan, cfg.ResetCameraTilt)
	}
}
