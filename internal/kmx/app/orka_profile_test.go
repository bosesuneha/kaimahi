package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestOrkaProfileSeparatesClientAndWorkerTimings(t *testing.T) {
	start := time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC)
	p := orkaTaskProfile{session: 200 * time.Millisecond, create: 50 * time.Millisecond,
		execution: 28 * time.Second, result: 100 * time.Millisecond, available: true}
	for i, entry := range []struct {
		kind string
		at   time.Duration
	}{
		{"TaskCreated", 0}, {"TaskJobCreated", 30 * time.Millisecond},
		{"WorkerStarted", time.Second}, {"ModelRequestStarted", 1100 * time.Millisecond},
		{"ModelRequestCompleted", 23 * time.Second}, {"ResultSubmitted", 23100 * time.Millisecond},
		{"WorkerCompleted", 23200 * time.Millisecond}, {"TaskSucceeded", 27 * time.Second},
	} {
		event := orkaTimingEvent{Type: entry.kind, CreatedAt: start.Add(entry.at)}
		if i == 4 {
			event.InputTokens, event.OutputTokens = 1035, 10
		}
		p.events = append(p.events, event)
	}
	summary := p.summary()
	for _, want := range []string{
		"Result connection: 200ms", "Task creation: 50ms", "Execution + completion wait: 28s",
		"Result retrieval: 100ms", "Within execution", "Job creation: 30ms", "Worker startup: 970ms",
		"Worker preparation: 100ms", "Model requests: 21.9s (calls: 1; 1035 input / 10 output tokens)",
		"Result submission: 100ms", "Worker cleanup: 100ms", "Task completion reporting: 3.8s",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("missing %q:\n%s", want, summary)
		}
	}
}

func TestOrkaProfileAccumulatesModelCallsWithoutCountingToolWait(t *testing.T) {
	start := time.Now()
	p := orkaTaskProfile{available: true, events: []orkaTimingEvent{
		{Type: "ModelRequestStarted", CreatedAt: start},
		{Type: "ModelRequestCompleted", CreatedAt: start.Add(time.Second), InputTokens: 100, OutputTokens: 10},
		{Type: "ModelRequestStarted", CreatedAt: start.Add(4 * time.Second)},
		{Type: "ModelRequestCompleted", CreatedAt: start.Add(6 * time.Second), InputTokens: 120, OutputTokens: 20},
	}}
	summary := p.summary()
	for _, want := range []string{"Model requests: 3s (calls: 2; 220 input / 30 output tokens)", "Between model calls (tools/events): 3s"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("missing %q:\n%s", want, summary)
		}
	}
}

func TestOrkaProfileFetchesOnlyTimingEventsAndHandlesUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotImplemented} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a, opt, _, _, _ := orkaCreateFixture(t, "")
			orkaResultServer(t, &opt, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/tasks/task/events" || r.URL.Query().Get("namespace") != opt.Namespace || !slices.Equal(r.URL.Query()["type"], orkaTimingTypes) {
					t.Errorf("unexpected timing request: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{"events": []orkaTimingEvent{{Type: "WorkerStarted", CreatedAt: time.Now()}}})
			})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := a.openOrkaResultSession(ctx, opt)
			if err != nil {
				t.Fatal(err)
			}
			defer session.close()
			p := &orkaTaskProfile{}
			p.loadEvents(ctx, session, opt.Namespace, "task")
			if p.available != (status == http.StatusOK) {
				t.Fatalf("available=%v", p.available)
			}
			if !p.available && !strings.Contains(p.summary(), "Worker phase timing unavailable") {
				t.Fatal("missing timing data presented as measured")
			}
		})
	}
}

func TestQuickstartOllamaPreloadDoesNotReplaceAgentPromptCache(t *testing.T) {
	f := newOrkaFixture(t, nil)
	f.app.Cfg.Model = "qwen2.5:3b"
	if err := f.app.prewarmQuickstartModel(); err != nil {
		t.Fatal(err)
	}
	if calls := f.calls(t); !strings.Contains(calls, "ollama run qwen2.5:3b\n") || strings.Contains(calls, "Reply with") {
		t.Fatalf("preload sends an unrelated prompt: %s", calls)
	}
	f.app.Cfg.ContainerEngine = "docker"
	fakeTool(t, f.dir, "docker", `printf '%s\n' "$*" >> "$KMX_TEST_ARGS"`)
	if err := f.app.prewarmHostQuickstartModel(&localModel{Provider: "ollama", Model: "test", Endpoint: "http://host:11434"}); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(filepath.Join(f.dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "/api/generate") || strings.Contains(string(calls), "messages") || strings.Contains(string(calls), "Reply with") {
		t.Fatalf("host preload sends an unrelated prompt: %s", calls)
	}
}
