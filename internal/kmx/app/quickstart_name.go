package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type quickstartNameCheck func(context.Context, string, string) error
type quickstartNameDebounce struct {
	revision        int
	name, namespace string
}
type quickstartNameResult struct {
	quickstartNameDebounce
	err error
}

func (a *App) checkQuickstartName(ctx context.Context, namespace, name string) error {
	for _, resource := range []string{"agents.core.orka.ai", "providers.core.orka.ai"} {
		raw, err := a.orkaCapture(ctx, nil, "-n", namespace, "get", resource, name, "--ignore-not-found=true", "-o", "name")
		if err != nil {
			return fmt.Errorf("Cannot check name right now; try again")
		}
		if strings.TrimSpace(string(raw)) != "" {
			return fmt.Errorf("Name %q already exists (%s); choose another name or use the existing agent", name, strings.Split(resource, ".")[0])
		}
	}
	return nil
}

func (m *quickstartWizardModel) scheduleNameCheck() tea.Cmd {
	if m.nameCheck == nil || m.create.step != createName || m.agentStep || m.modelStep {
		return nil
	}
	name, namespace := strings.TrimSpace(m.create.input.Value()), m.create.opt.Namespace
	if name == m.nameValue && namespace == m.nameNamespace {
		return nil
	}
	if m.nameCancel != nil {
		m.nameCancel()
		m.nameCancel = nil
	}
	m.nameRevision++
	m.nameValue, m.nameNamespace = name, namespace
	m.nameErr, m.nameChecked = nil, false
	if validateAgentName(name) != nil {
		m.namePending = false
		return nil
	}
	m.namePending = true
	msg := quickstartNameDebounce{m.nameRevision, name, namespace}
	return tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg { return msg })
}

func (m quickstartWizardModel) currentNameCheck(msg quickstartNameDebounce) bool {
	return m.create.step == createName && !m.quitting && msg.revision == m.nameRevision && msg.name == strings.TrimSpace(m.create.input.Value()) && msg.namespace == m.create.opt.Namespace
}
