package app

import (
	"context"
	"fmt"

	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

// Kagent retains its native session IDs, history and HITL continuation protocol.
// The terminal coordinator provides a decision callback; model/session work is
// behind the same Session contract as Orka. No Orka resource is involved.
type kagentRuntimeSession struct {
	app                                       *App
	executable, name, session, base, toolMode string
	posture                                   *chatGovernancePosture
	stop                                      func()
	decide                                    func(context.Context, *streamView, *chatRenderer) (*streamView, error)
	// Compatibility driver renders the native stream directly. Other consumers
	// use typed events through runtimeEventRenderer instead.
	renderer        *chatRenderer
	approvalFailure bool
	decisionFailure bool
}

func (s *kagentRuntimeSession) Agent() agentruntime.AgentRef {
	return agentruntime.AgentRef{Runtime: agentruntime.Kagent, Context: s.app.Cfg.KubeContext, Namespace: "kagent", Kind: "agents.kagent.dev", Name: s.name}
}
func (s *kagentRuntimeSession) Capabilities() agentruntime.Capabilities {
	return agentruntime.Capabilities{Streaming: true, Resume: true, Approvals: true}
}
func (s *kagentRuntimeSession) Commands() []agentruntime.Command {
	var commands []agentruntime.Command
	for _, command := range slashCommandList {
		if !containsChatCommand(commonChatCommands(), command.name) {
			commands = append(commands, agentruntime.Command{Name: command.name, Usage: command.usage})
		}
	}
	return commands
}
func (s *kagentRuntimeSession) output(emit agentruntime.Emit, verbose bool) *chatRenderer {
	if s.renderer != nil {
		return s.renderer
	}
	return runtimeEventRenderer(emit, verbose)
}
func (s *kagentRuntimeSession) Connect(ctx context.Context, emit agentruntime.Emit) (agentruntime.Status, error) {
	status := agentruntime.Status{Agent: s.Agent()}
	if err := ctx.Err(); err != nil {
		return status, err
	}
	if len(s.name) > 63 || !agentNameRE.MatchString(s.name) {
		return status, fmt.Errorf("agent name %q is not a valid Kubernetes name", s.name)
	}
	if err := s.app.waitServable(s.name); err != nil {
		return status, err
	}
	port, stop, err := s.app.portForward()
	if err != nil {
		return status, err
	}
	s.stop = stop
	s.base = "http://127.0.0.1:" + port
	r := s.output(emit, s.app.chatVerbose)
	s.posture, err = s.app.refreshChatPosture(s.name, r)
	if err != nil {
		s.Close()
		return status, err
	}
	if s.session != "" {
		if err = s.app.showSessionHistory(s.base, s.session, s.name, s.toolMode, r); err != nil {
			s.Close()
			return status, err
		}
	}
	return status, nil
}
func (s *kagentRuntimeSession) Send(ctx context.Context, turn agentruntime.Turn, emit agentruntime.Emit) error {
	s.approvalFailure, s.decisionFailure = false, false
	r := s.output(emit, turn.Verbose)
	r.beginAssistant(s.name)
	if s.posture != nil && s.posture.modelGoverned {
		r.assistantOperation(s.name, "KAIMAHI ROUTE", "", colorYellow, "Seam: model proxy\nConfiguration: verified through ready plane at chat start\nPer-call decision: not exposed by kagent stream")
	}
	view, err := s.app.invokeStream(ctx, s.executable, s.base, s.name, turn.Message, s.session, s.toolMode, r, s.posture)
	if view != nil && view.context != "" {
		s.session = view.context
	}
	if err != nil {
		return err
	}
	for view.approval != nil {
		s.approvalFailure = true
		if view.approvalErr != nil {
			return view.approvalErr
		}
		if s.decide == nil {
			return fmt.Errorf("kagent requires an approval/input handler; no decision submitted")
		}
		view, err = s.decide(ctx, view, r)
		if view != nil && view.context != "" {
			s.session = view.context
		}
		if err != nil {
			return err
		}
	}
	s.approvalFailure = false
	return nil
}
func (s *kagentRuntimeSession) Close() {
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}
