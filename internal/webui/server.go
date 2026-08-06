package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"

	"atomcam-rotctld/internal/rotator"
)

type Controller interface {
	Status() rotator.Status
	SetPosition(rotator.Position) error
	Park() error
	Reset() error
}

type Options struct {
	Address       string
	AtomCamURL    string
	RotctlAddress string
	Version       string
	ManualStep    float64
}

type Server struct {
	address       string
	controller    Controller
	atomCamURL    string
	rotctlAddress string
	version       string
	manualStep    float64
	resetMu       sync.Mutex
	resetRunning  bool
}

func New(controller Controller, options Options) *Server {
	return &Server{
		address:       options.Address,
		controller:    controller,
		atomCamURL:    options.AtomCamURL,
		rotctlAddress: options.RotctlAddress,
		version:       options.Version,
		manualStep:    options.ManualStep,
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/manual-move", s.handleManualMove)
	mux.HandleFunc("/api/park", s.handlePark)
	mux.HandleFunc("/api/reset", s.handleReset)

	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("web UI listening on %s", listener.Addr())

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) handleIndex(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write([]byte(indexHTML))
}

func (s *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.writeStatus(writer)
}

func (s *Server) handleManualMove(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var payload struct {
		Direction string `json:"direction"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON")
		return
	}
	current := s.controller.Status().Actual
	switch strings.ToLower(payload.Direction) {
	case "left":
		current.Azimuth = normalize360(current.Azimuth - s.manualStep)
	case "right":
		current.Azimuth = normalize360(current.Azimuth + s.manualStep)
	case "up":
		current.Elevation = clamp(current.Elevation+s.manualStep, 0, 90)
	case "down":
		current.Elevation = clamp(current.Elevation-s.manualStep, 0, 90)
	default:
		writeError(writer, http.StatusBadRequest, "direction must be left, right, up, or down")
		return
	}
	if err := s.controller.SetPosition(current); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	s.writeStatus(writer)
}

func (s *Server) handlePark(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.controller.Park(); err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeStatus(writer)
}

func (s *Server) handleReset(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.resetMu.Lock()
	if s.resetRunning {
		s.resetMu.Unlock()
		s.writeStatus(writer)
		return
	}
	s.resetRunning = true
	s.resetMu.Unlock()
	go func() {
		defer func() {
			s.resetMu.Lock()
			s.resetRunning = false
			s.resetMu.Unlock()
		}()
		if err := s.controller.Reset(); err != nil {
			log.Printf("web UI reset failed: %v", err)
		}
	}()
	writer.WriteHeader(http.StatusAccepted)
	s.writeStatus(writer)
}

func (s *Server) writeStatus(writer http.ResponseWriter) {
	status := s.controller.Status()
	response := struct {
		State         string           `json:"state"`
		Azimuth       float64          `json:"azimuth"`
		Elevation     float64          `json:"elevation"`
		ActualOK      bool             `json:"actual_ok"`
		Requested     rotator.Position `json:"requested"`
		RequestedOK   bool             `json:"requested_ok"`
		Moving        bool             `json:"moving"`
		Resetting     bool             `json:"resetting"`
		Tracking      bool             `json:"tracking"`
		LastError     string           `json:"last_error,omitempty"`
		LastMove      string           `json:"last_move,omitempty"`
		AtomCamURL    string           `json:"atomcam_url"`
		RotctlAddress string           `json:"rotctl_address"`
		Version       string           `json:"version"`
		ManualStep    float64          `json:"manual_step"`
	}{
		State:         status.State,
		Azimuth:       status.Actual.Azimuth,
		Elevation:     status.Actual.Elevation,
		ActualOK:      status.ActualOK,
		Requested:     status.Requested,
		RequestedOK:   status.RequestedOK,
		Moving:        status.Moving,
		Resetting:     status.Resetting,
		Tracking:      status.Tracking,
		LastError:     status.LastError,
		AtomCamURL:    s.atomCamURL,
		RotctlAddress: s.rotctlAddress,
		Version:       s.version,
		ManualStep:    s.manualStep,
	}
	if !status.LastMove.IsZero() {
		response.LastMove = status.LastMove.Format("2006-01-02T15:04:05Z07:00")
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(response)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": message})
}

func normalize360(value float64) float64 {
	value = math.Mod(value, 360)
	if value < 0 {
		value += 360
	}
	return value
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}

var indexHTML = fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>atomcam-rotctld</title>
<style>
:root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background:#0f172a; color:#e5e7eb; }
body { margin:0; min-height:100vh; background:radial-gradient(circle at top left, #1e293b, #020617 55%%); }
main { width:min(1080px, calc(100vw - 32px)); margin:0 auto; padding:28px 0 36px; }
.top { display:flex; justify-content:space-between; gap:16px; align-items:flex-start; margin-bottom:18px; }
h1 { font-size:24px; margin:0 0 6px; letter-spacing:0; }
.sub { color:#94a3b8; font-size:14px; word-break:break-all; }
.badge { padding:8px 12px; border:1px solid #334155; border-radius:999px; font-weight:700; background:#111827; }
.badge.TRACKING { color:#7dd3fc; border-color:#0284c7; }
.badge.RESETTING { color:#facc15; border-color:#ca8a04; }
.badge.MOVING { color:#a7f3d0; border-color:#059669; }
.grid { display:grid; grid-template-columns: 1.1fr .9fr; gap:18px; align-items:stretch; }
.panel { background:rgba(15,23,42,.82); border:1px solid #263449; border-radius:8px; padding:18px; box-shadow:0 18px 60px rgba(0,0,0,.28); }
.label { color:#94a3b8; font-size:12px; text-transform:uppercase; letter-spacing:.08em; }
.value { font-size:38px; line-height:1.1; margin-top:10px; font-variant-numeric:tabular-nums; text-align:center; }
.gauges { display:grid; grid-template-columns:1fr 1fr; gap:18px; align-items:start; }
.gauge { border:1px solid #243244; border-radius:10px; padding:16px; background:#0b1220; }
.gauge-plot { display:grid; place-items:center; min-height:270px; }
.gauge svg { width:min(280px, 100%%); height:auto; overflow:visible; }
.gauge-controls { display:grid; grid-template-columns:1fr 1fr; gap:10px; margin-top:14px; }
.needle { stroke:#fb3b32; stroke-width:6; stroke-linecap:round; filter:drop-shadow(0 2px 2px rgba(0,0,0,.5)); }
.needle-tail { stroke:#cbd5e1; stroke-width:2; stroke-linecap:round; }
.hub { fill:#fb3b32; stroke:#ef4444; stroke-width:2; }
.tick-text { fill:#e5e7eb; font-size:18px; font-weight:800; paint-order:stroke; stroke:#334155; stroke-width:3; stroke-linejoin:round; }
.axis { stroke:#334155; stroke-dasharray:5 7; }
.controls { display:grid; grid-template-columns:repeat(3, minmax(0,1fr)); gap:10px; margin-top:8px; }
button { appearance:none; border:1px solid #334155; background:#162033; color:#f8fafc; border-radius:8px; padding:13px 12px; font-size:16px; font-weight:700; cursor:pointer; }
button:hover { background:#1f2d46; }
button:active { transform:translateY(1px); }
button.primary { border-color:#0ea5e9; background:#075985; }
button.warn { border-color:#b45309; background:#78350f; }
.wide { grid-column:span 3; }
.meta { display:grid; gap:10px; margin-top:16px; }
.row { display:flex; justify-content:space-between; gap:14px; border-bottom:1px solid #1e293b; padding-bottom:8px; font-size:14px; }
.row span:first-child { color:#94a3b8; }
.row span:last-child { text-align:right; word-break:break-all; }
.error { color:#fecaca; margin-top:12px; min-height:20px; }
@media (max-width: 980px) { .grid { grid-template-columns:1fr; } }
@media (max-width: 640px) { .top { display:grid; } .gauges { grid-template-columns:1fr; } }
</style>
</head>
<body>
<main>
  <div class="top">
    <div>
      <h1>atomcam-rotctld</h1>
      <div class="sub">ATOM Cam: <span id="atomcam">-</span></div>
    </div>
    <div id="state" class="badge">STANDBY</div>
  </div>
  <div class="grid">
    <section class="panel">
      <div class="gauges">
        <div class="gauge">
          <div class="label">Azimuth</div>
          <div class="gauge-plot">
            <svg viewBox="0 0 260 260" role="img" aria-label="Azimuth gauge">
              <circle cx="130" cy="130" r="108" fill="#1f2937" stroke="#334155" stroke-width="2"/>
              <path d="M130 130 L130 22 A108 108 0 0 1 238 130 A108 108 0 0 1 130 238 A108 108 0 0 1 22 130 A108 108 0 0 1 130 22 Z" fill="#2f7d4f" opacity=".38"/>
              <circle cx="130" cy="130" r="78" fill="#0b1220" opacity=".55"/>
              <line class="axis" x1="130" y1="22" x2="130" y2="238"/>
              <line class="axis" x1="22" y1="130" x2="238" y2="130"/>
              <text class="tick-text" x="130" y="22" text-anchor="middle">0</text>
              <text class="tick-text" x="236" y="136" text-anchor="middle">90</text>
              <text class="tick-text" x="130" y="249" text-anchor="middle">180</text>
              <text class="tick-text" x="24" y="136" text-anchor="middle">270</text>
              <line id="azTail" class="needle-tail" x1="130" y1="130" x2="95" y2="95"/>
              <line id="azNeedle" class="needle" x1="130" y1="130" x2="130" y2="36"/>
              <circle class="hub" cx="130" cy="130" r="9"/>
            </svg>
          </div>
          <div class="value">AZ: <span id="az">0.0</span>°</div>
          <div class="gauge-controls">
            <button data-dir="left">↺ CCW</button>
            <button data-dir="right">CW ↻</button>
          </div>
        </div>
        <div class="gauge">
          <div class="label">Elevation</div>
          <div class="gauge-plot">
            <svg viewBox="0 0 260 190" role="img" aria-label="Elevation gauge">
              <path d="M40 150 L40 24 A126 126 0 0 1 166 150 Z" fill="#2f7d4f" opacity=".38" stroke="#334155" stroke-width="2"/>
              <path d="M40 150 L40 56 A94 94 0 0 1 134 150 Z" fill="#0b1220" opacity=".45"/>
              <path d="M40 150 L166 150 L166 174 L40 174 Z" fill="#7f2d2d" opacity=".72"/>
              <line class="axis" x1="40" y1="150" x2="166" y2="150"/>
              <line class="axis" x1="40" y1="150" x2="40" y2="24"/>
              <text class="tick-text" x="166" y="145" text-anchor="middle">0</text>
              <text class="tick-text" x="129" y="63" text-anchor="middle">45</text>
              <text class="tick-text" x="40" y="22" text-anchor="middle">90</text>
              <line id="elTail" class="needle-tail" x1="40" y1="150" x2="78" y2="150"/>
              <line id="elNeedle" class="needle" x1="40" y1="150" x2="150" y2="150"/>
              <circle class="hub" cx="40" cy="150" r="9"/>
            </svg>
          </div>
          <div class="value">EL: <span id="el">0.0</span>°</div>
          <div class="gauge-controls">
            <button data-dir="up">↑ UP</button>
            <button data-dir="down">DOWN ↓</button>
          </div>
        </div>
      </div>
    </section>
    <section class="panel">
      <div class="controls">
        <button class="primary wide" id="park">Park</button>
        <button class="warn wide" id="reset">Reset Position</button>
      </div>
      <div class="meta">
        <div class="row"><span>Tracking</span><span id="tracking">-</span></div>
        <div class="row"><span>Moving</span><span id="moving">-</span></div>
        <div class="row"><span>Resetting</span><span id="resetting">-</span></div>
        <div class="row"><span>Manual step</span><span id="step">-</span></div>
        <div class="row"><span>Rotctl</span><span id="rotctl">-</span></div>
        <div class="row"><span>Version</span><span id="version">-</span></div>
      </div>
      <div class="error" id="error"></div>
    </section>
  </div>
</main>
<script>
const azEl = { azimuth: 0, elevation: 0 };
const azNeedle = document.getElementById("azNeedle");
const azTail = document.getElementById("azTail");
const elNeedle = document.getElementById("elNeedle");
const elTail = document.getElementById("elTail");
const errorBox = document.getElementById("error");
function setText(id, value) { document.getElementById(id).textContent = value; }
function boolText(value) { return value ? "YES" : "NO"; }
function clamp(value, min, max) { return Math.max(min, Math.min(max, value)); }
function setLineEnd(line, x, y) {
  line.setAttribute("x2", x.toFixed(2));
  line.setAttribute("y2", y.toFixed(2));
}
function updateGauges(azimuth, elevation) {
  const az = ((azimuth %% 360) + 360) %% 360;
  const azRadians = az * Math.PI / 180;
  const azCx = 130;
  const azCy = 130;
  setLineEnd(azNeedle, azCx + Math.sin(azRadians) * 94, azCy - Math.cos(azRadians) * 94);
  setLineEnd(azTail, azCx - Math.sin(azRadians) * 38, azCy + Math.cos(azRadians) * 38);

  const el = clamp(elevation, 0, 90);
  const elRadians = el * Math.PI / 180;
  const elX = 40;
  const elY = 150;
  setLineEnd(elNeedle, elX + Math.cos(elRadians) * 110, elY - Math.sin(elRadians) * 110);
  setLineEnd(elTail, elX - Math.cos(elRadians) * 28, elY + Math.sin(elRadians) * 28);
}
async function request(path, options) {
  const response = await fetch(path, options);
  const payload = await response.json();
  if (!response.ok) throw new Error(payload.error || response.statusText);
  return payload;
}
function render(status) {
  azEl.azimuth = status.azimuth || 0;
  azEl.elevation = status.elevation || 0;
  setText("az", azEl.azimuth.toFixed(1));
  setText("el", azEl.elevation.toFixed(1));
  setText("atomcam", status.atomcam_url || "-");
  setText("tracking", boolText(status.tracking));
  setText("moving", boolText(status.moving));
  setText("resetting", boolText(status.resetting));
  setText("step", (status.manual_step || 0).toFixed(1) + "°");
  setText("rotctl", status.rotctl_address || "-");
  setText("version", status.version || "-");
  const badge = document.getElementById("state");
  badge.textContent = status.state || "STANDBY";
  badge.className = "badge " + (status.state || "STANDBY");
  errorBox.textContent = status.last_error || "";
  updateGauges(azEl.azimuth, azEl.elevation);
}
async function refresh() {
  try { render(await request("/api/status")); }
  catch (error) { errorBox.textContent = error.message; }
}
async function command(path, body) {
  try {
    render(await request(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : "{}"
    }));
  } catch (error) {
    errorBox.textContent = error.message;
  }
}
document.querySelectorAll("[data-dir]").forEach(button => {
  button.addEventListener("click", () => command("/api/manual-move", { direction: button.dataset.dir }));
});
document.getElementById("park").addEventListener("click", () => command("/api/park"));
document.getElementById("reset").addEventListener("click", () => command("/api/reset"));
refresh();
setInterval(refresh, 1000);
</script>
</body>
</html>`)
