package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestLiftDeploymentReusesMatchingResourcesAndResumesPartial(t *testing.T) {
	a, opt, _, _, dir := orkaCreateFixture(t, "lift-reuse")
	a.liftReuse = true
	if err := a.CreateAgent(opt); err != nil {
		t.Fatal(err)
	}
	if err := a.CreateAgent(opt); err != nil {
		t.Fatalf("repeat failed: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "sample-agents.core.orka.ai.json")); err != nil {
		t.Fatal(err)
	}
	if err := a.CreateAgent(opt); err != nil {
		t.Fatalf("partial resume failed: %v", err)
	}
	writes := map[string]int{}
	for _, call := range orkaCalls(t, dir) {
		if call.Document != nil && strings.Contains(strings.Join(call.Args, " "), "create") && !strings.Contains(strings.Join(call.Args, " "), "--dry-run=server") {
			writes[call.Document["kind"].(string)]++
		}
	}
	if writes["Provider"] != 1 || writes["Agent"] != 2 {
		t.Fatalf("writes=%v", writes)
	}
	opt.Model = "different-model"
	if err := a.CreateAgent(opt); err == nil || !strings.Contains(err.Error(), "different configuration") {
		t.Fatalf("conflict=%v", err)
	}
}

func TestLiftPaneRetryOnlyAfterFailure(t *testing.T) {
	m := liftDeployModel{}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
	if cmd != nil || updated.(liftDeployModel).retry {
		t.Fatal("retried active deployment")
	}
	m.done = true
	m.err = os.ErrPermission
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'r'})
	if cmd == nil || !updated.(liftDeployModel).retry || !updated.(liftDeployModel).quitting {
		t.Fatal("failure retry unavailable")
	}
}
