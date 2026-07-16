package rotator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"atomcam-rotctld/internal/atomcam"
)

type Device interface {
	Move(context.Context, float64, float64, int, int) (atomcam.Position, error)
	Position(context.Context) (atomcam.Position, error)
	Reset(context.Context) error
}

type Controller struct {
	device              Device
	transform           Transform
	speed               int
	priority            int
	threshold           float64
	moveTimeout         time.Duration
	resetTimeout        time.Duration
	park                Position
	resetCameraPosition *CameraPosition

	mu            sync.RWMutex
	resetMu       sync.Mutex
	submitMu      sync.Mutex
	requested     Position
	requestedOK   bool
	actual        Position
	actualOK      bool
	moving        bool
	resetting     bool
	resetDone     chan struct{}
	lastError     error
	lastMove      time.Time
	pending       chan Position
	workerContext context.Context
}

type Options struct {
	Speed               int
	Priority            int
	Threshold           float64
	MoveTimeout         time.Duration
	ResetTimeout        time.Duration
	Park                Position
	ResetCameraPosition *CameraPosition
}

func New(device Device, transform Transform, options Options) *Controller {
	return &Controller{
		device:              device,
		transform:           transform,
		speed:               options.Speed,
		priority:            options.Priority,
		threshold:           options.Threshold,
		moveTimeout:         options.MoveTimeout,
		resetTimeout:        options.ResetTimeout,
		park:                options.Park,
		resetCameraPosition: options.ResetCameraPosition,
		pending:             make(chan Position, 1),
	}
}

func (c *Controller) Start(ctx context.Context) {
	c.workerContext = ctx
	go c.worker(ctx)

	queryCtx, cancel := context.WithTimeout(ctx, c.moveTimeout)
	defer cancel()
	devicePosition, err := c.device.Position(queryCtx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.lastError = err
		log.Printf("initial camera position query failed: %v", err)
		return
	}
	c.actual = c.transform.FromCamera(CameraPosition{Pan: devicePosition.Pan, Tilt: devicePosition.Tilt})
	c.actualOK = true
	log.Printf("initial camera position: azimuth=%.2f elevation=%.2f", c.actual.Azimuth, c.actual.Elevation)
}

func (c *Controller) SetPosition(pos Position) error {
	if pos.Azimuth < 0 || pos.Azimuth > 360 || pos.Elevation < 0 || pos.Elevation > 90 {
		return fmt.Errorf("position outside azimuth 0..360 or elevation 0..90")
	}
	pos.Azimuth = normalize360(pos.Azimuth)

	c.submitMu.Lock()
	defer c.submitMu.Unlock()
	c.mu.Lock()
	if c.requestedOK && angularDistance(c.requested.Azimuth, pos.Azimuth) < c.threshold && abs(c.requested.Elevation-pos.Elevation) < c.threshold {
		c.mu.Unlock()
		return nil
	}
	c.requested = pos
	c.requestedOK = true
	c.mu.Unlock()

	select {
	case c.pending <- pos:
	default:
		select {
		case <-c.pending:
		default:
		}
		c.pending <- pos
	}
	return nil
}

func (c *Controller) Position() Position {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.actualOK {
		return c.actual
	}
	if c.requestedOK {
		return c.requested
	}
	return Position{}
}

func (c *Controller) Reset() error {
	c.resetMu.Lock()
	defer c.resetMu.Unlock()

	c.mu.Lock()
	resetDone := make(chan struct{})
	c.resetDone = resetDone
	c.resetting = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.resetting = false
		close(resetDone)
		c.mu.Unlock()
	}()

	c.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), c.resetTimeout)
	defer cancel()
	log.Print("camera axis reset started")
	if err := c.device.Reset(ctx); err != nil {
		c.recordError(err)
		return err
	}
	var devicePosition atomcam.Position
	var err error
	if c.resetCameraPosition != nil {
		log.Printf("moving camera to post-reset raw position: pan=%.2f tilt=%.2f", c.resetCameraPosition.Pan, c.resetCameraPosition.Tilt)
		devicePosition, err = c.device.Move(ctx, c.resetCameraPosition.Pan, c.resetCameraPosition.Tilt, c.speed, c.priority)
	} else {
		devicePosition, err = c.device.Position(ctx)
	}
	if err != nil {
		c.recordError(err)
		return err
	}
	logical := c.transform.FromCamera(CameraPosition{Pan: devicePosition.Pan, Tilt: devicePosition.Tilt})
	c.mu.Lock()
	c.actual = logical
	c.actualOK = true
	c.lastError = nil
	c.mu.Unlock()
	log.Printf("camera axis reset complete: azimuth=%.2f elevation=%.2f camera_pan=%.2f camera_tilt=%.2f", logical.Azimuth, logical.Elevation, devicePosition.Pan, devicePosition.Tilt)
	return nil
}

func (c *Controller) Stop() {
	c.submitMu.Lock()
	defer c.submitMu.Unlock()
	select {
	case <-c.pending:
	default:
	}
}

func (c *Controller) Park() error {
	return c.SetPosition(c.park)
}

func (c *Controller) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case target := <-c.pending:
			if !c.waitForReset(ctx) {
				return
			}
			cameraTarget := c.transform.ToCamera(target)
			c.mu.Lock()
			c.moving = true
			c.mu.Unlock()
			moveCtx, cancel := context.WithTimeout(ctx, c.moveTimeout)
			devicePosition, err := c.device.Move(moveCtx, cameraTarget.Pan, cameraTarget.Tilt, c.speed, c.priority)
			cancel()
			c.mu.Lock()
			c.moving = false
			c.lastMove = time.Now()
			if err != nil {
				c.lastError = err
				log.Printf("camera move failed for azimuth=%.2f elevation=%.2f: %v", target.Azimuth, target.Elevation, err)
			} else {
				c.actual = c.transform.FromCamera(CameraPosition{Pan: devicePosition.Pan, Tilt: devicePosition.Tilt})
				c.actualOK = true
				c.lastError = nil
				log.Printf("camera move complete: azimuth=%.2f elevation=%.2f camera_pan=%.2f camera_tilt=%.2f", c.actual.Azimuth, c.actual.Elevation, devicePosition.Pan, devicePosition.Tilt)
			}
			c.mu.Unlock()
		}
	}
}

func (c *Controller) waitForReset(ctx context.Context) bool {
	c.mu.RLock()
	resetting := c.resetting
	resetDone := c.resetDone
	c.mu.RUnlock()
	if !resetting || resetDone == nil {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-resetDone:
		return true
	}
}

func (c *Controller) recordError(err error) {
	c.mu.Lock()
	c.lastError = err
	c.mu.Unlock()
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
