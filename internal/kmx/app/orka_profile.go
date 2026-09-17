package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client durations use a monotonic clock. Worker phases use Orka's event-store
// timestamps and are a breakdown of execution, not additional client latency.
type orkaTaskProfile struct {
	session, create, execution, result, telemetry time.Duration
	events                                        []orkaTimingEvent
	available                                     bool
}

type orkaTimingEvent struct {
	Type         string    `json:"type"`
	CreatedAt    time.Time `json:"createdAt"`
	InputTokens  int       `json:"inputTokens"`
	OutputTokens int       `json:"outputTokens"`
}

var orkaTimingTypes = []string{"TaskCreated", "TaskJobCreated", "WorkerStarted", "ModelRequestStarted", "ModelRequestCompleted", "ResultSubmitted", "WorkerCompleted", "TaskSucceeded"}

func (p *orkaTaskProfile) loadEvents(ctx context.Context, session *orkaResultSession, namespace, name string) {
	// Fetch only timing events, after the answer has passed all result checks.
	// Profiling is best-effort and never turns a valid answer into an error.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	query := url.Values{"type": orkaTimingTypes, "limit": {"100"}}
	status, body, err := session.getTaskResource(ctx, namespace, name, "events", query)
	if err == nil && status == http.StatusOK && json.Unmarshal(body["events"], &p.events) == nil && len(p.events) > 0 && len(p.events) < 100 {
		p.available = true
	}
}

func (p *orkaTaskProfile) summary() string {
	duration := func(d time.Duration) string { return d.Round(time.Millisecond).String() }
	lines := []string{
		"Result connection: " + duration(p.session),
		"Task creation: " + duration(p.create),
		"Execution + completion wait: " + duration(p.execution),
		"Result retrieval: " + duration(p.result),
		"Timing lookup: " + duration(p.telemetry),
	}
	if !p.available {
		return strings.Join(append(lines, "Worker phase timing unavailable."), "\n")
	}
	first, last := map[string]time.Time{}, map[string]time.Time{}
	var modelStart time.Time
	var modelTime time.Duration
	var calls, input, output int
	for _, event := range p.events {
		if event.CreatedAt.IsZero() {
			continue
		}
		if first[event.Type].IsZero() {
			first[event.Type] = event.CreatedAt
		}
		last[event.Type] = event.CreatedAt
		if event.Type == "ModelRequestStarted" {
			modelStart = event.CreatedAt
		}
		if event.Type == "ModelRequestCompleted" && !modelStart.IsZero() && !event.CreatedAt.Before(modelStart) {
			modelTime += event.CreatedAt.Sub(modelStart)
			calls++
			input += event.InputTokens
			output += event.OutputTokens
			modelStart = time.Time{}
		}
	}
	lines = append(lines, "Within execution (Orka event timestamps):")
	add := func(label string, from, to time.Time) {
		if !from.IsZero() && !to.IsZero() && !to.Before(from) {
			lines = append(lines, "  "+label+": "+duration(to.Sub(from)))
		}
	}
	add("Job creation", first["TaskCreated"], first["TaskJobCreated"])
	add("Worker startup", first["TaskJobCreated"], first["WorkerStarted"])
	add("Worker preparation", first["WorkerStarted"], first["ModelRequestStarted"])
	if calls > 0 {
		lines = append(lines, fmt.Sprintf("  Model requests: %s (calls: %d; %d input / %d output tokens)", duration(modelTime), calls, input, output))
	}
	if calls > 1 {
		span := last["ModelRequestCompleted"].Sub(first["ModelRequestStarted"])
		if span >= modelTime {
			lines = append(lines, "  Between model calls (tools/events): "+duration(span-modelTime))
		}
	}
	add("Result submission", last["ModelRequestCompleted"], last["ResultSubmitted"])
	add("Worker cleanup", last["ResultSubmitted"], last["WorkerCompleted"])
	add("Task completion reporting", last["WorkerCompleted"], last["TaskSucceeded"])
	return strings.Join(lines, "\n")
}
