package webui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"atomcam-rotctld/internal/rotator"
)

type fakeController struct {
	status    rotator.Status
	set       rotator.Position
	setCalled bool
	parked    bool
	reset     chan struct{}
}

func (f *fakeController) Status() rotator.Status {
	return f.status
}

func (f *fakeController) SetPosition(position rotator.Position) error {
	f.set = position
	f.setCalled = true
	f.status.Actual = position
	f.status.ActualOK = true
	return nil
}

func (f *fakeController) Park() error {
	f.parked = true
	return nil
}

func (f *fakeController) Reset() error {
	f.reset <- struct{}{}
	return nil
}

func TestStatusEndpoint(t *testing.T) {
	controller := &fakeController{status: rotator.Status{
		State:    "TRACKING",
		Actual:   rotator.Position{Azimuth: 123, Elevation: 45},
		ActualOK: true,
		Tracking: true,
	}}
	server := New(controller, Options{
		AtomCamURL:    "http://192.0.2.1",
		RotctlAddress: "0.0.0.0:4533",
		Version:       "test",
		ManualStep:    5,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	recorder := httptest.NewRecorder()
	server.handleStatus(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{`"state":"TRACKING"`, `"azimuth":123`, `"elevation":45`, `"atomcam_url":"http://192.0.2.1"`} {
		if !bytes.Contains([]byte(body), []byte(want)) {
			t.Fatalf("response %s does not contain %s", body, want)
		}
	}
}

func TestManualMoveEndpoint(t *testing.T) {
	controller := &fakeController{status: rotator.Status{
		State:    "STANDBY",
		Actual:   rotator.Position{Azimuth: 10, Elevation: 20},
		ActualOK: true,
	}}
	server := New(controller, Options{ManualStep: 5})
	request := httptest.NewRequest(http.MethodPost, "/api/manual-move", bytes.NewBufferString(`{"direction":"up"}`))
	recorder := httptest.NewRecorder()
	server.handleManualMove(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !controller.setCalled {
		t.Fatal("SetPosition was not called")
	}
	if controller.set.Azimuth != 10 || controller.set.Elevation != 25 {
		t.Fatalf("set position = %+v, want az=10 el=25", controller.set)
	}
}

func TestResetEndpointReturnsAccepted(t *testing.T) {
	controller := &fakeController{reset: make(chan struct{}, 1)}
	server := New(controller, Options{ManualStep: 5})
	request := httptest.NewRequest(http.MethodPost, "/api/reset", bytes.NewBufferString(`{}`))
	recorder := httptest.NewRecorder()
	server.handleReset(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status code = %d", recorder.Code)
	}
	select {
	case <-controller.reset:
	case <-time.After(time.Second):
		t.Fatal("reset was not triggered")
	}
}
