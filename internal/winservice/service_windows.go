//go:build windows

package winservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	Name        = "PlantOpsEdgeScale"
	DisplayName = "PlantOps Edge Scale"
	Description = "Offline-first PlantOps unmanned truck scale edge controller"
)

type RunFunc func(context.Context) error

func IsService() (bool, error) { return svc.IsWindowsService() }

func Run(fn RunFunc) error {
	if fn == nil { return errors.New("service run function is nil") }
	if exe, err := os.Executable(); err == nil { _ = os.Chdir(filepath.Dir(exe)) }
	return svc.Run(Name, &handler{run: fn})
}

type handler struct{ run RunFunc }

func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status <- svc.Status{State: svc.StartPending}
	errCh := make(chan error, 1)
	go func() { errCh <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: accepts}
	for {
		select {
		case err := <-errCh:
			if err != nil { return false, 1 }
			return false, 0
		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case err := <-errCh:
					if err != nil { return false, 1 }
					return false, 0
				case <-time.After(25 * time.Second):
					return false, 2
				}
			}
		}
	}
}

func Install(args []string) error {
	exe, err := os.Executable(); if err != nil { return err }
	exe, err = filepath.Abs(exe); if err != nil { return err }
	m, err := mgr.Connect(); if err != nil { return err }; defer m.Disconnect()
	if existing, err := m.OpenService(Name); err == nil { existing.Close(); return fmt.Errorf("service %s already exists", Name) }
	s, err := m.CreateService(Name, exe, mgr.Config{
		DisplayName: DisplayName, Description: Description,
		StartType: mgr.StartAutomatic, ErrorControl: mgr.ErrorNormal,
		DelayedAutoStart: true,
	}, args...)
	if err != nil { return err }
	defer s.Close()
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 24*60*60); err != nil { _ = s.Delete(); return fmt.Errorf("set service recovery: %w", err) }
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil { _ = s.Delete(); return fmt.Errorf("set service recovery flag: %w", err) }
	return nil
}

func Start() error { s, m, err := open(); if err != nil { return err }; defer m.Disconnect(); defer s.Close(); return s.Start() }

func Stop(timeout time.Duration) error {
	if timeout <= 0 { timeout = 30 * time.Second }
	s, m, err := open(); if err != nil { return err }; defer m.Disconnect(); defer s.Close()
	st, err := s.Query(); if err != nil { return err }
	if st.State == svc.Stopped { return nil }
	if _, err := s.Control(svc.Stop); err != nil { return err }
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, err = s.Query(); if err != nil { return err }
		if st.State == svc.Stopped { return nil }
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service %s did not stop within %s", Name, timeout)
}

func Uninstall() error {
	s, m, err := open(); if err != nil { return err }; defer m.Disconnect(); defer s.Close()
	st, qerr := s.Query(); if qerr == nil && st.State != svc.Stopped { return fmt.Errorf("service %s must be stopped before uninstall", Name) }
	return s.Delete()
}

func Status() (string, error) {
	s, m, err := open(); if err != nil { return "", err }; defer m.Disconnect(); defer s.Close()
	st, err := s.Query(); if err != nil { return "", err }
	switch st.State {
	case svc.Stopped: return "STOPPED", nil
	case svc.StartPending: return "START_PENDING", nil
	case svc.StopPending: return "STOP_PENDING", nil
	case svc.Running: return "RUNNING", nil
	case svc.ContinuePending: return "CONTINUE_PENDING", nil
	case svc.PausePending: return "PAUSE_PENDING", nil
	case svc.Paused: return "PAUSED", nil
	default: return fmt.Sprintf("STATE_%d", st.State), nil
	}
}

func open() (*mgr.Service, *mgr.Mgr, error) {
	m, err := mgr.Connect(); if err != nil { return nil, nil, err }
	s, err := m.OpenService(Name); if err != nil { m.Disconnect(); return nil, nil, err }
	return s, m, nil
}

func StripManagementArg(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-service" || a == "--service" { if i+1 < len(args) { i++ }; continue }
		if strings.HasPrefix(a, "-service=") || strings.HasPrefix(a, "--service=") { continue }
		out = append(out, a)
	}
	return out
}
