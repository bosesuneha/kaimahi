package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLiftLoadingCentersBelowHeader(t *testing.T) {
	m := liftLoadingModel{label: "Fetching clusters", header: &liftHeader{agent: "demo", source: "kind-local"}, width: 100, height: 24, started: time.Now()}
	view := m.View().Content
	if lipgloss.Width(view) > 100 || lipgloss.Height(view) > 24 {
		t.Fatalf("view overflows: %dx%d", lipgloss.Width(view), lipgloss.Height(view))
	}
	lines := strings.Split(view, "\n")
	top := -1
	for i, line := range lines {
		if strings.Contains(line, "╭") {
			top = i
			break
		}
	}
	if top < 5 || top > 14 {
		t.Fatalf("box not centered below header: row=%d", top)
	}
	if !strings.Contains(view, "Fetching clusters") {
		t.Fatal("loading label missing")
	}
}

func TestLiftLoadingScopeSegmentsUseSeparateRows(t *testing.T) {
	subscription := strings.Repeat("a", 8) + "-" + strings.Repeat("b", 4) + "-" + strings.Repeat("c", 4) + "-" + strings.Repeat("d", 4) + "-" + strings.Repeat("e", 12)
	m := liftLoadingModel{label: "Fetching AKS credentials · subscription:" + subscription + " · rg:demo-rg · resource:demo-aks", width: 100, height: 24, started: time.Now()}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Fetching AKS credentials", "subscription: " + subscription, "rg: demo-rg", "resource:demo-aks"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing complete segment %q:\n%s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "subscription:") && strings.Contains(line, "rg:") {
			t.Fatal("scope fields share a row")
		}
	}
}

func TestAgentConnectionLoadingKeepsHeaderAndAnimates(t *testing.T) {
	m := liftLoadingModel{label: "Checking Agent identity and current-generation readiness", width: 60, height: 20, started: time.Now(), statusHeader: func(width int) string { return agentConnectionHeader("remote-agent", "aks-prod", "Connecting", width) }}
	first := m.View().Content
	updated, _ := m.Update(quickstartTickMsg{})
	second := updated.(liftLoadingModel).View().Content
	if first == second {
		t.Fatal("pending spinner did not advance")
	}
	for _, want := range []string{"remote-agent", "aks-prod", "Connecting", "Checking Agent"} {
		if !strings.Contains(second, want) {
			t.Fatalf("missing %q: %s", want, second)
		}
	}
	if lipgloss.Height(second) > 20 || lipgloss.Width(second) > 60 {
		t.Fatalf("connection pane overflows: %dx%d", lipgloss.Width(second), lipgloss.Height(second))
	}
}

func TestLiftLoadingReturnsFetchFailure(t *testing.T) {
	want := errors.New("fetch failed")
	_, err := runLiftLoading(t.Context(), strings.NewReader(""), &bytes.Buffer{}, "Loading", nil, func(context.Context) ([]byte, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}

func TestLiftLoadingCancellationWaitsForFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := liftLoadingModel{cancel: cancel}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil || !updated.(liftLoadingModel).cancelled || ctx.Err() == nil {
		t.Fatal("did not cancel fetch before quitting")
	}
	stopped := make(chan struct{})
	_, err := runLiftLoading(t.Context(), strings.NewReader("\x03"), &bytes.Buffer{}, "Loading", nil, func(ctx context.Context) ([]byte, error) { <-ctx.Done(); close(stopped); return nil, ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("fetch still running after pane exit")
	}
}
