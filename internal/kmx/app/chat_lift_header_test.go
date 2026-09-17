package app

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLiftHeaderKeepsTargetAndHighlightsStage(t *testing.T) {
	h := liftHeader{agent: "demo", source: "kind-local", target: "aks-prod", step: 2}
	for _, width := range []int{40, 100} {
		view := h.view(width)
		if !strings.Contains(ansi.Strip(view), "agent:demo") || !strings.Contains(ansi.Strip(view), "target:aks-prod") || !strings.Contains(view, "Inference") {
			t.Fatalf("missing status: %s", view)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("header overflows: %q", line)
			}
		}
		if !strings.Contains(view, "\x1b[1;30;46m") {
			t.Fatal("current stage not highlighted")
		}
	}
}

func TestLiftPickerRetainsHeaderWhileFiltering(t *testing.T) {
	h := &liftHeader{agent: "demo", source: "kind-local", step: 0}
	m := chatPicker{header: h, title: "AKS clusters", width: 80, height: 24, searchEnabled: true, items: []chatPickerItem{{name: "aks-a"}, {name: "aks-b"}}}
	first := m.View().Content
	m.query = "aks-b"
	second := m.View().Content
	if lipgloss.Height(first) != lipgloss.Height(second) || !strings.Contains(ansi.Strip(second), "KMX / agent:demo") {
		t.Fatal("filtering lost header or resized pane")
	}
	if lipgloss.Height(second) >= 24 {
		t.Fatalf("pane too tall: %d", lipgloss.Height(second))
	}
}
