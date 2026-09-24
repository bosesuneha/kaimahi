package app

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

// A third implementation deliberately knows nothing about Orka or Kubernetes.
type thirdRuntimeSession struct {
	messages []string
	commands int
	closed   bool
}

func osCreateRuntimeInput(t *testing.T, text string) (*os.File, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { f.Close() })
	if _, err = f.WriteString(text); err != nil {
		return nil, err
	}
	_, err = f.Seek(0, 0)
	return f, err
}
func (s *thirdRuntimeSession) Agent() agentruntime.AgentRef {
	return agentruntime.AgentRef{Runtime: "third", Name: "third-agent", Context: "test", Namespace: "custom"}
}
func (s *thirdRuntimeSession) Capabilities() agentruntime.Capabilities {
	return agentruntime.Capabilities{Streaming: true}
}
func (s *thirdRuntimeSession) Commands() []agentruntime.Command {
	return []agentruntime.Command{{Name: "/inspect", Usage: "/inspect — third runtime info"}}
}
func (s *thirdRuntimeSession) Connect(ctx context.Context, emit agentruntime.Emit) (agentruntime.Status, error) {
	emit(agentruntime.Event{Kind: agentruntime.Connection, Agent: s.Agent().Name, Text: "test"})
	return agentruntime.Status{Agent: s.Agent(), Fields: []agentruntime.Field{{Label: "Runtime", Value: "third"}}}, nil
}
func (s *thirdRuntimeSession) Send(ctx context.Context, turn agentruntime.Turn, emit agentruntime.Emit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.messages = append(s.messages, turn.Message)
	emit(agentruntime.Event{Kind: agentruntime.AgentStarted, Agent: s.Agent().Name})
	emit(agentruntime.Event{Kind: agentruntime.Text, Agent: s.Agent().Name, Text: "third reply", Start: true})
	return nil
}
func (s *thirdRuntimeSession) Close() { s.closed = true }
func (s *thirdRuntimeSession) Execute(ctx context.Context, command string, emit agentruntime.Emit) (agentruntime.CommandResult, error) {
	s.commands++
	return agentruntime.CommandResult{Refresh: true}, nil
}

func TestRuntimeThirdAdapterCommandsEventsAndPlainShell(t *testing.T) {
	s := &thirdRuntimeSession{}
	backend := &runtimeChatBackend{session: s}
	a := chatUXFixture(t)
	a.Out, a.Err = io.Discard, io.Discard
	// Use the existing scanner fixture to exercise actual shared dispatch.
	input, err := osCreateRuntimeInput(t, "hello\n/inspect\n/retry\n/exit\n")
	if err != nil {
		t.Fatal(err)
	}
	a.Stdin = input
	if err := a.runInteractiveChatBackend(backend); err != nil {
		t.Fatal(err)
	}
	if len(s.messages) != 2 || s.commands != 1 || !s.closed {
		t.Fatalf("messages=%v commands=%d closed=%t", s.messages, s.commands, s.closed)
	}
	commands := chatBackendCommands(backend)
	if containsChatCommand(commands, "/lift") || containsChatCommand(commands, "/tools") || !containsChatCommand(commands, "/inspect") {
		t.Fatal("Orka commands leaked")
	}
	m := newChatTimeline("third-agent", "test", "", false)
	m.commands = commands
	m.editor.SetValue("/inspect")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if updated.(chatTimelineModel).command != "/inspect" {
		t.Fatal("third command not dispatched by timeline")
	}
}

type probeAdapter struct {
	id    agentruntime.ID
	err   error
	found bool
	calls *int
}

func (a probeAdapter) ID() agentruntime.ID { return a.id }
func (a probeAdapter) Probe(ctx context.Context, target agentruntime.Target) (agentruntime.Probe, error) {
	*a.calls++
	return agentruntime.Probe{Found: a.found, Agent: agentruntime.AgentRef{Runtime: a.id, Name: target.Name}}, a.err
}
func (a probeAdapter) Open(context.Context, agentruntime.Target) (agentruntime.Session, error) {
	return nil, errors.New("not used")
}
func TestRuntimeRegistryNeverFallsBackOnReadFailure(t *testing.T) {
	first, second := 0, 0
	registrations := []runtimeRegistration{{adapter: probeAdapter{id: "first", err: errors.New("access denied"), calls: &first}}, {adapter: probeAdapter{id: "second", found: true, calls: &second}}}
	if _, err := resolveRegisteredRuntime(t.Context(), registrations, agentruntime.Target{Name: "same-name"}); err == nil || second != 0 {
		t.Fatal("read failure fell through")
	}
	registrations[0].adapter = probeAdapter{id: "first", calls: &first}
	ref, err := resolveRegisteredRuntime(t.Context(), registrations, agentruntime.Target{Name: "same-name"})
	if err != nil || ref.Runtime != "second" {
		t.Fatal("absence did not permit fallback")
	}
}

func TestRuntimeInferenceStrategyIsSeparateFromPlatform(t *testing.T) {
	for _, mode := range []string{"local", "copilot", "foundry", "unknown"} {
		a := &App{chatInference: mode, foundryClient: &foundryChatClient{}}
		strategy, host, err := a.hostInferenceStrategy()
		if mode == "unknown" {
			if err == nil {
				t.Fatal("unknown inference silently defaulted")
			}
			continue
		}
		if err != nil || host != (mode != "local") || (host && strategy == nil) {
			t.Fatalf("mode=%s host=%t err=%v", mode, host, err)
		}
	}
	s := &thirdRuntimeSession{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Send(ctx, agentruntime.Turn{Message: "never"}, func(agentruntime.Event) {}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var text strings.Builder
	r := newChatRenderer(&text)
	emitToRenderer(r)(agentruntime.Event{Kind: agentruntime.Text, Agent: "third", Text: "typed event", Start: true})
	r.finish()
	if !strings.Contains(text.String(), "typed event") {
		t.Fatal("event not rendered")
	}
}
