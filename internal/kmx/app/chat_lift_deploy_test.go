package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestLiftDeploymentStagesAreSequentialAndFailureRemainsVisible(t *testing.T) {
	m := liftDeployModel{title: "Deploy Agent", target: "aks-demo", agent: "demo", started: time.Now(), stages: []liftDeployEvent{{name: "Create Provider", status: "pending"}, {name: "Wait for Provider Ready", status: "pending"}, {name: "Create Agent", status: "pending"}}}
	for _, event := range []liftDeployEvent{{name: "Create Provider", status: "active"}, {name: "Create Provider", status: "done"}, {name: "Wait for Provider Ready", status: "active"}, {name: "Wait for Provider Ready", status: "failed"}} {
		updated, _ := m.Update(event)
		m = updated.(liftDeployModel)
	}
	updated, cmd := m.Update(liftDeployEvent{status: "complete", err: errors.New("readiness failed")})
	m = updated.(liftDeployModel)
	if cmd != nil || !m.done || m.quitting || m.stages[2].status != "pending" {
		t.Fatalf("model=%+v", m)
	}
	if !strings.Contains(m.View().Content, "readiness failed") {
		t.Fatal("diagnostic not shown")
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !updated.(liftDeployModel).quitting {
		t.Fatal("acknowledgement did not exit")
	}
}

func TestLiftDeploymentCancelWaitsForWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	m := liftDeployModel{cancel: cancel, started: time.Now()}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m = updated.(liftDeployModel)
	if ctx.Err() == nil || !m.cancelled || cmd != nil {
		t.Fatal("cancellation quit before worker joined")
	}
	updated, cmd = m.Update(liftDeployEvent{status: "complete", err: context.Canceled})
	if cmd == nil || !updated.(liftDeployModel).quitting {
		t.Fatal("worker completion did not release cancellation")
	}
}

func TestLiftDeploymentPaneFitsSmallWindow(t *testing.T) {
	m := liftDeployModel{title: "Install Orka", target: "long-remote-cluster-name", agent: "hello-world-agent", started: time.Now(), width: 40, height: 12, stages: []liftDeployEvent{{name: "Fetch the pinned installer", status: "active"}, {name: "Reconcile the wrapper credential", status: "pending"}, {name: "Apply the installer and wait", status: "pending"}}}
	view := m.View().Content
	if lipgloss.Height(view) > 12 {
		t.Fatalf("too tall:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("too wide: %q", line)
		}
	}
	if !strings.Contains(view, "Fetch the pinned installer") {
		t.Fatal("active stage hidden")
	}
}

func TestOrkaOnlineReportsRealDeploymentStages(t *testing.T) {
	a, opt, _, _, _ := orkaCreateFixture(t, "")
	var stages []string
	a.operationProgress = func(name, status string, _ error) { stages = append(stages, name+":"+status) }
	if err := a.CreateAgent(opt); err != nil {
		t.Fatal(err)
	}
	want := []string{"Validate schemas and prerequisites:active", "Validate schemas and prerequisites:done", "Validate server admission:active", "Validate server admission:done", "Create Provider:active", "Create Provider:done", "Wait for Provider Ready:active", "Wait for Provider Ready:done", "Create Agent:active", "Create Agent:done", "Wait for Agent Ready:active", "Wait for Agent Ready:done"}
	if strings.Join(stages, "|") != strings.Join(want, "|") {
		t.Fatalf("stages=%v", stages)
	}
}
