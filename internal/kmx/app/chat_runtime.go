package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Resolve by API group, never by the friendly Agent name alone. A failed read
// must not silently connect the caller to a different runtime.
func (a *App) resolveInteractiveChat(opt ChatOptions, name string) (string, string, error) {
	namespace := opt.Namespace
	if opt.Runtime == "kagent" {
		if namespace != "" && namespace != "kagent" {
			return "", "", fmt.Errorf("kagent chat requires namespace kagent")
		}
		return "kagent", "kagent", nil
	}
	if namespace == "" {
		namespace = OrkaNamespace
	}
	if opt.Runtime == "orka" {
		return "orka", namespace, nil
	}
	ctx, cancel := context.WithTimeout(a.operationContext(), 15*time.Second)
	defer cancel()
	// Discovery avoids mistaking an uninstalled Orka CRD for a failed Agent read.
	raw, err := a.orkaCapture(ctx, nil, "api-resources", "--api-group=core.orka.ai", "-o", "name")
	if err != nil {
		return "", "", fmt.Errorf("cannot discover chat runtimes: %w; select --runtime explicitly", err)
	}
	hasOrka := false
	for _, resource := range strings.Fields(string(raw)) {
		if resource == "agents.core.orka.ai" {
			hasOrka = true
		}
	}
	if hasOrka {
		raw, err = a.orkaCapture(ctx, nil, "-n", namespace, "get", "agents.core.orka.ai", name, "--ignore-not-found=true", "-o", "json")
		if err != nil {
			return "", "", fmt.Errorf("cannot inspect Orka Agent %s/%s: %w", namespace, name, err)
		}
		if len(strings.TrimSpace(string(raw))) > 0 {
			var agent struct {
				Metadata struct{ Name, Namespace string }
				Spec     struct{ Runtime json.RawMessage }
			}
			if json.Unmarshal(raw, &agent) != nil || agent.Metadata.Name != name || agent.Metadata.Namespace != namespace {
				return "", "", fmt.Errorf("invalid Orka Agent identity")
			}
			if len(agent.Spec.Runtime) > 0 && string(agent.Spec.Runtime) != "null" {
				return "", "", fmt.Errorf("Agent %s uses an external CLI runtime; this chat supports Orka AI agents", name)
			}
			return "orka", namespace, nil
		}
	}
	if opt.Namespace != "" && opt.Namespace != "kagent" {
		return "", "", fmt.Errorf("Orka Agent %s/%s was not found", namespace, name)
	}
	return "kagent", "kagent", nil
}
