package rotctld

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"atomcam-rotctld/internal/rotator"
)

type fakeRotator struct {
	position           rotator.Position
	reset              bool
	resetSignal        chan struct{}
	sessionResetSignal chan struct{}
	stopped            bool
	parked             bool
}

func (f *fakeRotator) SetPosition(position rotator.Position) error {
	f.position = position
	return nil
}
func (f *fakeRotator) Position() rotator.Position { return f.position }
func (f *fakeRotator) Reset() error {
	f.reset = true
	if f.resetSignal != nil {
		select {
		case f.resetSignal <- struct{}{}:
		default:
		}
	}
	return nil
}
func (f *fakeRotator) SessionReset() error {
	if f.sessionResetSignal != nil {
		select {
		case f.sessionResetSignal <- struct{}{}:
		default:
		}
	}
	return nil
}
func (f *fakeRotator) Stop() { f.stopped = true }
func (f *fakeRotator) Park() error {
	f.parked = true
	return nil
}

func TestHamlibInitializationAndPositionCommands(t *testing.T) {
	rot := &fakeRotator{}
	server := New(":0", rot, Options{})

	if got := runCommand(server, "\\dump_state"); got != "1\n2\nmin_az=0.000000\nmax_az=360.000000\nmin_el=0.000000\nmax_el=90.000000\nsouth_zero=0\nrot_type=AzEl\ndone\n" {
		t.Fatalf("unexpected dump_state:\n%s", got)
	}
	if got := runCommand(server, "P 163.0 41.0"); got != "RPRT 0\n" {
		t.Fatalf("unexpected set response: %q", got)
	}
	if rot.position.Azimuth != 163 || rot.position.Elevation != 41 {
		t.Fatalf("unexpected position: %+v", rot.position)
	}
	if got := runCommand(server, "p"); got != "163.000000\n41.000000\n" {
		t.Fatalf("unexpected get response: %q", got)
	}
}

func TestControlCommands(t *testing.T) {
	rot := &fakeRotator{}
	server := New(":0", rot, Options{})
	for _, command := range []string{"R 1", "S", "K"} {
		if got := runCommand(server, command); got != "RPRT 0\n" {
			t.Errorf("%s returned %q", command, got)
		}
	}
	if !rot.reset || !rot.stopped || !rot.parked {
		t.Fatalf("commands not dispatched: %+v", rot)
	}
	if got := runCommand(server, "M 2 50"); got != "RPRT -4\n" {
		t.Fatalf("manual move returned %q", got)
	}
}

func TestSessionResetRunsAsynchronously(t *testing.T) {
	rot := &fakeRotator{sessionResetSignal: make(chan struct{}, 1)}
	server := New(":0", rot, Options{ResetOnSession: true, ResetSessionMode: "before"})
	server.triggerPreSessionReset()
	select {
	case <-rot.sessionResetSignal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session reset")
	}
}

func TestPostSessionResetRunsAfterDumpStateConnectionCloses(t *testing.T) {
	rot := &fakeRotator{resetSignal: make(chan struct{}, 1)}
	server := New(":0", rot, Options{ResetOnSession: true, ResetSessionMode: "after"})
	client, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.handleConnection(serverConn)
		close(done)
	}()

	if _, err := client.Write([]byte("\\dump_state\n")); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "done" {
			break
		}
	}
	select {
	case <-rot.resetSignal:
		t.Fatal("post-session reset ran before connection closed")
	default:
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connection handler")
	}
	select {
	case <-rot.resetSignal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for post-session reset")
	}
}

func runCommand(server *Server, command string) string {
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	server.handleCommand(strings.TrimSpace(command), writer)
	_ = writer.Flush()
	return output.String()
}
