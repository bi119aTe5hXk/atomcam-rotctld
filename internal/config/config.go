package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddress   string
	AtomCamURL      string
	PanOffset       float64
	PanScale        float64
	TiltHorizon     float64
	TiltScale       float64
	MoveSpeed       int
	MovePriority    int
	MoveThreshold   float64
	MoveTimeout     time.Duration
	ResetTimeout    time.Duration
	ResetOnStart    bool
	ResetOnSession  bool
	ResetCameraPan  float64
	ResetCameraTilt float64
	LogCommands     bool
	ParkAzimuth     float64
	ParkElevation   float64
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress:   envString("LISTEN_ADDRESS", "0.0.0.0:4533"),
		AtomCamURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("ATOMCAM_URL")), "/"),
		PanOffset:       envFloat("PAN_OFFSET", 180),
		PanScale:        envFloat("PAN_SCALE", 1),
		TiltHorizon:     envFloat("TILT_HORIZON", 0),
		TiltScale:       envFloat("TILT_SCALE", 1),
		MoveSpeed:       envInt("MOVE_SPEED", 5),
		MovePriority:    envInt("MOVE_PRIORITY", 1),
		MoveThreshold:   envFloat("MOVE_THRESHOLD", 3),
		MoveTimeout:     envDuration("MOVE_TIMEOUT", 20*time.Second),
		ResetTimeout:    envDuration("RESET_TIMEOUT", 90*time.Second),
		ResetOnStart:    envBool("RESET_ON_START", false),
		ResetOnSession:  envBool("RESET_ON_SESSION", false),
		ResetCameraPan:  envFloat("RESET_CAMERA_PAN", 180),
		ResetCameraTilt: envFloat("RESET_CAMERA_TILT", 0),
		LogCommands:     envBool("LOG_COMMANDS", true),
		ParkAzimuth:     envFloat("PARK_AZIMUTH", 0),
		ParkElevation:   envFloat("PARK_ELEVATION", 0),
	}

	if cfg.AtomCamURL == "" {
		return Config{}, fmt.Errorf("ATOMCAM_URL is required")
	}
	if cfg.PanScale != -1 && cfg.PanScale != 1 {
		return Config{}, fmt.Errorf("PAN_SCALE must be 1 or -1")
	}
	if cfg.TiltScale != -1 && cfg.TiltScale != 1 {
		return Config{}, fmt.Errorf("TILT_SCALE must be 1 or -1")
	}
	if cfg.MoveSpeed < 1 || cfg.MoveSpeed > 9 {
		return Config{}, fmt.Errorf("MOVE_SPEED must be between 1 and 9")
	}
	if cfg.MovePriority < 0 || cfg.MovePriority > 3 {
		return Config{}, fmt.Errorf("MOVE_PRIORITY must be between 0 and 3")
	}
	if cfg.MoveThreshold < 0 {
		return Config{}, fmt.Errorf("MOVE_THRESHOLD must not be negative")
	}
	if (cfg.ResetCameraPan >= 0) != (cfg.ResetCameraTilt >= 0) {
		return Config{}, fmt.Errorf("RESET_CAMERA_PAN and RESET_CAMERA_TILT must both be set or both be disabled with -1")
	}
	if cfg.ResetCameraPan > 355 || cfg.ResetCameraTilt > 180 {
		return Config{}, fmt.Errorf("reset camera position must be within pan 0..355 and tilt 0..180")
	}
	if cfg.ParkAzimuth < 0 || cfg.ParkAzimuth > 360 || cfg.ParkElevation < 0 || cfg.ParkElevation > 90 {
		return Config{}, fmt.Errorf("park position must be within azimuth 0..360 and elevation 0..90")
	}
	return cfg, nil
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
