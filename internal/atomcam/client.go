package atomcam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type Position struct {
	Pan  float64
	Tilt float64
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	mu         sync.Mutex
}

func New(rawURL string) (*Client, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse ATOMCAM_URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("ATOMCAM_URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("ATOMCAM_URL must include a host")
	}
	return &Client{baseURL: parsed, httpClient: &http.Client{}}, nil
}

func (c *Client) Move(ctx context.Context, pan, tilt float64, speed, priority int) (Position, error) {
	command := fmt.Sprintf("move %.3f %.3f %d %d", pan, tilt, speed, priority)
	response, err := c.command(ctx, command, true)
	if err != nil {
		return Position{}, err
	}
	return parsePosition(response)
}

func (c *Client) Position(ctx context.Context) (Position, error) {
	response, err := c.command(ctx, "move", true)
	if err != nil {
		return Position{}, err
	}
	return parsePosition(response)
}

func (c *Client) Reset(ctx context.Context) error {
	response, err := c.command(ctx, "moveinit", false)
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(response), "error") {
		return fmt.Errorf("atomcam reset failed: %s", response)
	}
	return nil
}

func (c *Client) command(ctx context.Context, command string, socket bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	payload, err := json.Marshal(map[string]string{"exec": command})
	if err != nil {
		return "", err
	}

	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/cgi-bin/cmd.cgi"
	if socket {
		query := endpoint.Query()
		query.Set("port", "socket")
		endpoint.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create atomcam request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("send atomcam command %q: %w", command, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("read atomcam response: %w", err)
	}
	response := strings.TrimSpace(string(body))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("atomcam returned HTTP %d: %s", res.StatusCode, response)
	}
	if response == "" {
		return "", fmt.Errorf("atomcam returned an empty response")
	}
	if strings.HasPrefix(strings.ToLower(response), "error") {
		return "", fmt.Errorf("atomcam command %q failed: %s", command, response)
	}
	return response, nil
}

func parsePosition(response string) (Position, error) {
	fields := strings.Fields(response)
	if len(fields) < 2 {
		return Position{}, fmt.Errorf("unexpected atomcam position response %q", response)
	}
	pan, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return Position{}, fmt.Errorf("parse atomcam pan from %q: %w", response, err)
	}
	tilt, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return Position{}, fmt.Errorf("parse atomcam tilt from %q: %w", response, err)
	}
	return Position{Pan: pan, Tilt: tilt}, nil
}
