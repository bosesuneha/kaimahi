package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestChatToolPickerSearchToggleAndLockedTools(t *testing.T) {
	m := chatPicker{multiple: true, searchEnabled: true, searching: true, items: []chatPickerItem{
		{name: "k8s-get-resources"}, {name: "remember", enabled: true, locked: true},
	}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k', Text: "k8s"})
	m = updated.(chatPicker)
	if got := m.matches(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("matches=%v", got)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(chatPicker)
	if !m.items[0].enabled {
		t.Fatal("tool was not enabled")
	}
	m.query = "remember"
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if !updated.(chatPicker).items[1].enabled {
		t.Fatal("automatic memory tool was toggled")
	}
}

func TestChatPickerCancelAndCompletionRestoreReader(t *testing.T) {
	for _, keys := range []string{"\x03", "\x1b", "\r"} {
		m, err := runChatPicker(context.Background(), strings.NewReader(keys), &bytes.Buffer{}, chatPicker{items: []chatPickerItem{{name: "demo"}}})
		if keys == "\x03" {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Ctrl-C error=%v", err)
			}
		} else if err != nil || m.accepted != (keys == "\r") {
			t.Fatalf("keys=%q accepted=%v err=%v", keys, m.accepted, err)
		}
	}
}

func TestChatToolPatchPreservesReferencesAndVersion(t *testing.T) {
	current := []map[string]any{{"name": "one", "enabled": true}, {"name": "two", "enabled": false}}
	items := []chatPickerItem{{name: "one", enabled: false}, {name: "two", enabled: true}, {name: "new", enabled: true}, {name: "remember", enabled: true, locked: true}}
	body, err := chatToolSelectionPatch("42", current, items)
	if err != nil {
		t.Fatal(err)
	}
	var patch []struct {
		Op, Path string
		Value    json.RawMessage
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		t.Fatal(err)
	}
	if len(patch) != 2 || patch[0].Op != "test" || string(patch[0].Value) != `"42"` {
		t.Fatalf("patch=%s", body)
	}
	var refs []map[string]any
	if err := json.Unmarshal(patch[1].Value, &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0]["enabled"] != false || refs[1]["enabled"] != true || refs[2]["name"] != "new" || current[0]["enabled"] != true {
		t.Fatalf("refs=%v current=%v", refs, current)
	}
	body, err = chatToolSelectionPatch("42", refs, items)
	if err != nil || body != nil {
		t.Fatalf("unchanged selection generated patch=%s err=%v", body, err)
	}
}

func TestPickerCaseInsensitiveFuzzyMatchesAndRanks(t *testing.T) {
	m := chatPicker{query: "AkPr", searchEnabled: true, items: []chatPickerItem{{name: "unrelated"}, {name: "AKS-production"}, {name: "akpr-exact"}, {name: "azure-kube-prod"}}}
	got := m.matches()
	if len(got) != 3 || got[0] != 2 {
		t.Fatalf("matches=%v", got)
	}
	m.query = "PROD azure"
	got = m.matches()
	if len(got) != 1 || got[0] != 3 {
		t.Fatalf("word search=%v", got)
	}
}

func TestPickerStartsInSearchAndEnterSelects(t *testing.T) {
	m, err := runChatPicker(t.Context(), strings.NewReader("kpr\r"), &bytes.Buffer{}, chatPicker{vim: true, searchEnabled: true, searchDefault: true, items: []chatPickerItem{{name: "alpha"}, {name: "kube-production"}}})
	if err != nil || !m.accepted || m.query != "kpr" || m.matches()[m.selection] != 1 {
		t.Fatalf("picker=%+v err=%v", m, err)
	}
}

func TestPickerSelectedRowHasDistinctBackground(t *testing.T) {
	m := chatPicker{items: []chatPickerItem{{name: "first"}, {name: "selected"}}, selection: 1, width: 60}
	view := m.View().Content
	if !strings.Contains(view, "\x1b[1;30;46m› selected") {
		t.Fatalf("selected row missing distinct style:\n%s", view)
	}
	if strings.Contains(view, "\x1b[1;30;46m  first") {
		t.Fatal("unselected row highlighted")
	}
}

func TestActionPickerCannotSearchOrFilterAwayCancel(t *testing.T) {
	m := chatPicker{vim: true, items: []chatPickerItem{{name: "Cancel"}, {name: "Create"}}}
	for _, key := range []tea.KeyPressMsg{{Code: '/', Text: "/"}, {Code: 'c', Text: "c"}} {
		updated, _ := m.Update(key)
		m = updated.(chatPicker)
	}
	if m.searching || m.query != "" || len(m.matches()) != 2 || strings.Contains(m.View().Content, "Search:") {
		t.Fatalf("action picker search enabled: %+v", m)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(chatPicker)
	if m.selection != 1 {
		t.Fatal("navigation unavailable on action picker")
	}
}

func TestPickerSearchCanBeAvailableWithoutStartingActive(t *testing.T) {
	m := chatPicker{searchEnabled: true, items: []chatPickerItem{{name: "alpha"}, {name: "beta"}}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m = updated.(chatPicker)
	if m.query != "" {
		t.Fatal("inactive search consumed typing")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(chatPicker)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m = updated.(chatPicker)
	if m.query != "b" || len(m.matches()) != 1 {
		t.Fatal("opt-in search did not work")
	}
}

func TestPickerDimensionsStayFixedWhileFiltering(t *testing.T) {
	for _, size := range [][2]int{{100, 32}, {60, 24}, {40, 14}} {
		m := chatPicker{title: "LIFT · AKS cluster", width: size[0], height: size[1], searchEnabled: true, searching: true, vim: true}
		for i := 0; i < 30; i++ {
			m.items = append(m.items, chatPickerItem{name: fmt.Sprintf("cluster-%02d", i), detail: "resource-group · region"})
		}
		initial := m.View().Content
		w, h := lipgloss.Width(initial), lipgloss.Height(initial)
		for _, query := range []string{"cl", "cluster-09", "nothing-matches", strings.Repeat("query", 40), ""} {
			m.query = query
			view := m.View().Content
			if lipgloss.Width(view) != w || lipgloss.Height(view) != h {
				t.Fatalf("size=%v query=%q dimensions changed: %dx%d -> %dx%d", size, query, w, h, lipgloss.Width(view), lipgloss.Height(view))
			}
			if w > size[0] || h >= size[1] {
				t.Fatalf("pane exceeds terminal: %dx%d in %v", w, h, size)
			}
			if strings.Count(view, "╭") != 1 || strings.Count(view, "╰") != 1 {
				t.Fatal("multiple frames")
			}
		}
	}
}
