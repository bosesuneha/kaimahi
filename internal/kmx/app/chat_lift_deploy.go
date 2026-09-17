package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type liftDeployEvent struct {
	name, status string
	err          error
}

type liftDeployModel struct {
	retry                     bool
	header                    *liftHeader
	title, target, agent      string
	stages                    []liftDeployEvent
	events                    <-chan liftDeployEvent
	cancel                    context.CancelFunc
	started                   time.Time
	frame, width, height      int
	done, cancelled, quitting bool
	err                       error
}

func waitLiftDeploy(events <-chan liftDeployEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return liftDeployEvent{status: "complete"}
		}
		return event
	}
}
func (m liftDeployModel) Init() tea.Cmd { return tea.Batch(waitLiftDeploy(m.events), quickstartTick()) }
func (m liftDeployModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case quickstartTickMsg:
		m.frame++
		if !m.done {
			return m, quickstartTick()
		}
	case createWizardCancelMsg:
		m.cancelled = true
		m.cancel()
		if m.done {
			m.quitting = true
			return m, tea.Quit
		}
	case tea.KeyPressMsg:
		if msg.Code == 'r' && m.done && m.err != nil && !m.cancelled {
			m.retry, m.quitting = true, true
			return m, tea.Quit
		}
		if msg.String() == "ctrl+c" || msg.Code == tea.KeyEsc {
			if m.done {
				m.quitting = true
				return m, tea.Quit
			}
			m.cancelled = true
			m.cancel()
		} else if msg.Code == tea.KeyEnter && m.done {
			m.quitting = true
			return m, tea.Quit
		}
	case liftDeployEvent:
		if msg.status == "complete" {
			m.done = true
			m.err = msg.err
			if m.cancelled {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
		found := false
		for i := range m.stages {
			if m.stages[i].name == msg.name {
				m.stages[i] = msg
				found = true
				break
			}
		}
		if !found {
			m.stages = append(m.stages, msg)
		}
		return m, waitLiftDeploy(m.events)
	}
	return m, nil
}
func (m liftDeployModel) View() tea.View {
	width, height := m.width, m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	header := ""
	if m.header != nil {
		header = m.header.view(width) + "\n"
		height = max(8, height-3)
	}
	inner := max(1, min(96, width)-6)
	line := func(s string) string {
		return ansi.Truncate(strings.Join(strings.Fields(safeTerminal(s)), " "), inner, "…")
	}
	rows := []string{line("LIFT / " + m.title), ansi.Truncate(tuiField("agent", m.agent)+" · "+tuiField("target", m.target), inner, "…"), ""}
	count := max(1, height-11)
	active := 0
	for i, stage := range m.stages {
		if stage.status == "active" || stage.status == "failed" {
			active = i
			break
		}
		if stage.status == "done" {
			active = i
		}
	}
	start := max(0, active-count+1)
	for _, stage := range m.stages[start:min(len(m.stages), start+count)] {
		marker := "·"
		switch stage.status {
		case "active":
			marker = []string{"⠋", "⠙", "⠹", "⠸"}[m.frame%4]
		case "done":
			marker = "✓"
		case "failed":
			marker = "✗"
		case "skipped":
			marker = "↪"
		}
		label := stage.name
		if stage.status == "skipped" {
			label += " (reused)"
		}
		rows = append(rows, quickstartStatusStyle(stage.status).Render(line(marker+" "+label)))
	}
	status := "Elapsed: " + formatElapsed(time.Since(m.started)) + " · ctrl+c cancel"
	if m.cancelled {
		status = "Cancelling active work…"
	} else if m.done {
		status = "Completed · enter return to lift"
		if m.err != nil {
			status = "Failed · r retry · enter return to chat"
		}
	}
	rows = append(rows, "", ansi.Truncate(tuiDetailLine(status), inner, "…"))
	if m.err != nil {
		rows = append(rows, line(m.err.Error()))
	}
	panel := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Blue).Padding(0, 1).Width(inner + 4).Render(strings.Join(rows, "\n"))
	return tea.NewView(header + panel)
}

// This pane owns stdin/output only during non-interactive deployment work.
// Join the worker before restoring chat so cancelled commands cannot write late.
func (b *orkaChatBackend) runLiftDeployment(parent context.Context, target *App, title string, stages []string, work func(*App) error) error {
	for {
		retry, err := b.runLiftDeploymentAttempt(parent, target, title, stages, work)
		if !retry || parent.Err() != nil {
			return err
		}
	}
}

func (b *orkaChatBackend) runLiftDeploymentAttempt(parent context.Context, target *App, title string, stages []string, work func(*App) error) (bool, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	worker := *target
	runner := *target.Run
	runner.Context = ctx
	runner.Stdout, runner.Stderr = io.Discard, io.Discard
	worker.Run = &runner
	worker.Out, worker.Err, worker.Stdin = io.Discard, io.Discard, nil
	events := make(chan liftDeployEvent, 32)
	stopped := make(chan struct{})
	done := make(chan error, 1)
	worker.operationProgress = func(name, status string, err error) {
		select {
		case events <- liftDeployEvent{name: name, status: status, err: err}:
		case <-stopped:
		}
	}
	m := liftDeployModel{title: title, target: target.Cfg.KubeContext, agent: b.agent, events: events, cancel: cancel, started: time.Now()}
	m.header = b.liftHeader
	if m.header != nil {
		m.width, m.height = prepareLiftPane(b.app.Out)
	}
	for _, name := range stages {
		m.stages = append(m.stages, liftDeployEvent{name: name, status: "pending"})
	}
	go func() {
		err := work(&worker)
		done <- err
		select {
		case events <- liftDeployEvent{status: "complete", err: err}:
		case <-stopped:
		}
		close(events)
	}()
	filter := func(model tea.Model, msg tea.Msg) tea.Msg {
		switch msg.(type) {
		case tea.InterruptMsg:
			return createWizardCancelMsg{}
		case tea.QuitMsg:
			if !model.(liftDeployModel).quitting {
				return createWizardCancelMsg{}
			}
		}
		return msg
	}
	result, runErr := tea.NewProgram(m, tea.WithInput(b.app.Stdin), tea.WithOutput(b.app.Out), tea.WithContext(parent), tea.WithFilter(filter)).Run()
	cancel()
	close(stopped)
	workErr := <-done
	if isInteractiveTerminal(b.app.Out) {
		fmt.Fprint(b.app.Out, "\x1b[H\x1b[2J")
	}
	b.paintLiftHeader()
	if parent.Err() != nil {
		return false, parent.Err()
	}
	if completed, ok := result.(liftDeployModel); ok && completed.cancelled {
		return false, context.Canceled
	}
	if runErr != nil {
		return false, runErr
	}
	completed, ok := result.(liftDeployModel)
	return ok && completed.retry, workErr
}
