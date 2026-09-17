package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
	"golang.org/x/sys/unix"
)

func TestLiftDeploymentPTYCancelStopsWorkAndRestoresTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	master, slave := chatPTY(t, 80)
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Cfg: &config.Config{KubeContext: "aks-target"}, Run: &run.Runner{}, Stdin: slave, Out: slave}
	b := &orkaChatBackend{app: a, agent: "demo"}
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		done <- b.runLiftDeployment(t.Context(), a, "Install Orka", []string{"Wait for runtime"}, func(worker *App) error {
			return worker.runPhase(phase{name: "Wait for runtime", current: 1, total: 1}, func() error {
				<-worker.operationContext().Done()
				close(stopped)
				return worker.operationContext().Err()
			})
		})
	}()
	var captured strings.Builder
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Contains(s, "Wait for runtime") })
	_, _ = io.WriteString(master, "\x03")
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pane did not cancel")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("returned before worker stopped")
	}
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *before != *after {
		t.Fatalf("terminal not restored: %v", err)
	}
}
