package rotctld

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"atomcam-rotctld/internal/rotator"
)

const (
	rigOK     = 0
	rigEINVAL = -1
	rigENIMPL = -4
	rigEIO    = -6
)

type Rotator interface {
	SetPosition(rotator.Position) error
	Position() rotator.Position
	Reset() error
	SessionReset() error
	SetTracking(bool)
	Stop()
	Park() error
}

type Server struct {
	address         string
	rotator         Rotator
	resetOnSession  bool
	logCommands     bool
	resetInProgress atomic.Bool
}

type Options struct {
	ResetOnSession bool
	LogCommands    bool
}

func New(address string, rot Rotator, options Options) *Server {
	return &Server{
		address:        address,
		rotator:        rot,
		resetOnSession: options.ResetOnSession,
		logCommands:    options.LogCommands,
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("rotctld listening on %s", listener.Addr())

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	var connections sync.WaitGroup
	defer connections.Wait()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			s.handleConnection(conn)
		}()
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()
	log.Printf("rotctld client connected: remote=%s", remote)
	defer log.Printf("rotctld client disconnected: remote=%s", remote)
	scanner := bufio.NewScanner(conn)
	writer := bufio.NewWriter(conn)
	trackingSession := false
	beforeSessionResetTriggered := false
	defer func() {
		if s.resetOnSession && resetsAfterSession(s.resetSessionMode) && trackingSession {
			s.triggerPostSessionReset()
		}
		if trackingSession {
			s.rotator.SetTracking(false)
		}
	}()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if s.logCommands && line != "p" && line != "\\get_pos" {
			log.Printf("rotctld command: remote=%s command=%q", remote, line)
		}
		if line == "\\dump_state" {
			if !trackingSession {
				trackingSession = true
				s.rotator.SetTracking(true)
			}
			if s.resetOnSession && resetsBeforeSession(s.resetSessionMode) && !beforeSessionResetTriggered {
				beforeSessionResetTriggered = true
				s.triggerPreSessionReset()
			}
		}
		closeConnection := s.handleCommand(line, writer)
		if err := writer.Flush(); err != nil || closeConnection {
			return
		}
	}
}

func (s *Server) triggerSessionReset() {
	if !s.resetInProgress.CompareAndSwap(false, true) {
		log.Print("session reset already in progress; reusing current reset")
		return
	}
	log.Print("Hamlib session opened; starting configured camera reset")
	go func() {
		defer s.resetInProgress.Store(false)
		if err := s.rotator.Reset(); err != nil {
			log.Printf("session camera reset failed: %v", err)
		}
	}()
}

func (s *Server) handleCommand(line string, writer *bufio.Writer) bool {
	fields := strings.Fields(line)
	command := fields[0]
	args := fields[1:]

	switch command {
	case "q", "\\quit":
		return true
	case "\\dump_state":
		fmt.Fprint(writer, "1\n2\nmin_az=0.000000\nmax_az=360.000000\nmin_el=0.000000\nmax_el=90.000000\nsouth_zero=0\nrot_type=AzEl\ndone\n")
	case "P", "\\set_pos":
		if len(args) != 2 {
			writeReport(writer, rigEINVAL)
			break
		}
		azimuth, azErr := strconv.ParseFloat(args[0], 64)
		elevation, elErr := strconv.ParseFloat(args[1], 64)
		if azErr != nil || elErr != nil {
			writeReport(writer, rigEINVAL)
			break
		}
		if err := s.rotator.SetPosition(rotator.Position{Azimuth: azimuth, Elevation: elevation}); err != nil {
			writeReport(writer, rigEINVAL)
			break
		}
		writeReport(writer, rigOK)
	case "p", "\\get_pos":
		position := s.rotator.Position()
		fmt.Fprintf(writer, "%.6f\n%.6f\n", position.Azimuth, position.Elevation)
	case "R", "\\reset":
		if len(args) != 1 || args[0] != "1" {
			writeReport(writer, rigEINVAL)
			break
		}
		if err := s.rotator.Reset(); err != nil {
			writeReport(writer, rigEIO)
			break
		}
		writeReport(writer, rigOK)
	case "S", "\\stop":
		s.rotator.Stop()
		writeReport(writer, rigOK)
	case "K", "\\park":
		if err := s.rotator.Park(); err != nil {
			writeReport(writer, rigEIO)
			break
		}
		writeReport(writer, rigOK)
	case "_", "\\get_info":
		fmt.Fprintln(writer, "ATOM Cam Swing via atomcam_tools")
	case "M", "\\move", "1", "\\dump_caps":
		writeReport(writer, rigENIMPL)
	default:
		writeReport(writer, rigENIMPL)
	}
	return false
}

func writeReport(writer *bufio.Writer, code int) {
	fmt.Fprintf(writer, "RPRT %d\n", code)
}
