package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"atomcam-rotctld/internal/atomcam"
	"atomcam-rotctld/internal/config"
	"atomcam-rotctld/internal/rotator"
	"atomcam-rotctld/internal/rotctld"
)

var version = "dev"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lmicroseconds)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			if err := healthcheck(); err != nil {
				log.Print(err)
				os.Exit(1)
			}
			return
		case "version":
			fmt.Println(version)
			return
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	device, err := atomcam.New(cfg.AtomCamURL)
	if err != nil {
		log.Fatal(err)
	}

	controller := rotator.New(device, rotator.Transform{
		PanOffset:   cfg.PanOffset,
		PanScale:    cfg.PanScale,
		TiltHorizon: cfg.TiltHorizon,
		TiltScale:   cfg.TiltScale,
	}, rotator.Options{
		Speed:               cfg.MoveSpeed,
		Priority:            cfg.MovePriority,
		Threshold:           cfg.MoveThreshold,
		MoveTimeout:         cfg.MoveTimeout,
		ResetTimeout:        cfg.ResetTimeout,
		Park:                rotator.Position{Azimuth: cfg.ParkAzimuth, Elevation: cfg.ParkElevation},
		ResetCameraPosition: resetCameraPosition(cfg),
	})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	controller.Start(ctx)
	if cfg.ResetOnStart {
		log.Print("resetting camera axes on startup")
		if err := controller.Reset(); err != nil {
			log.Fatalf("startup reset failed: %v", err)
		}
	}

	log.Printf("starting atomcam-rotctld version=%s atomcam=%s", version, cfg.AtomCamURL)
	server := rotctld.New(cfg.ListenAddress, controller, rotctld.Options{
		ResetOnSession:   cfg.ResetOnSession,
		ResetSessionMode: cfg.ResetSessionMode,
		LogCommands:      cfg.LogCommands,
	})
	if err := server.ListenAndServe(ctx); err != nil {
		log.Fatal(err)
	}
}

func resetCameraPosition(cfg config.Config) *rotator.CameraPosition {
	if cfg.ResetCameraPan < 0 || cfg.ResetCameraTilt < 0 {
		return nil
	}
	return &rotator.CameraPosition{Pan: cfg.ResetCameraPan, Tilt: cfg.ResetCameraTilt}
}

func healthcheck() error {
	address := strings.TrimSpace(os.Getenv("HEALTHCHECK_ADDRESS"))
	if address == "" {
		address = "127.0.0.1:4533"
	}
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return fmt.Errorf("connect to rotctld: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	if _, err := fmt.Fprint(conn, "p\n"); err != nil {
		return err
	}
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() || !scanner.Scan() {
		return fmt.Errorf("rotctld did not return azimuth and elevation")
	}
	return scanner.Err()
}
