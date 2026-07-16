package atomcam

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestMoveAndResetCommands(t *testing.T) {
	var commands []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, payload["exec"])
		if payload["exec"] == "moveinit" {
			if request.URL.Query().Get("port") != "" {
				t.Errorf("reset must use the web command path, got query %q", request.URL.RawQuery)
			}
			return response("moveinit OK\n"), nil
		}
		if request.URL.Query().Get("port") != "socket" {
			t.Errorf("move must use socket port")
		}
		return response("120.000000 45.000000 0 0\n"), nil
	})

	client, err := New("http://atomcam.local")
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = transport
	position, err := client.Move(context.Background(), 120, 45, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	if position.Pan != 120 || position.Tilt != 45 {
		t.Fatalf("unexpected position: %+v", position)
	}
	if err := client.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0] != "move 120.000 45.000 5 1" || commands[1] != "moveinit" {
		t.Fatalf("unexpected commands: %#v", commands)
	}
}

func TestRejectsErrorResponse(t *testing.T) {
	client, err := New("http://atomcam.local")
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response("error : canceled\n"), nil
	})
	if _, err := client.Position(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func response(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
