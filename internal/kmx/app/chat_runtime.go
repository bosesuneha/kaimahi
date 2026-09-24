package app

import (
	"fmt"

	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

// Explicit selection avoids discovery. Auto uses ordered adapters; errors never
// fall through to another platform with a same-named Agent.
func (a *App) resolveInteractiveChat(opt ChatOptions, name string) (string, string, error) {
	if opt.Runtime == "kagent" {
		if opt.Namespace != "" && opt.Namespace != "kagent" {
			return "", "", fmt.Errorf("kagent chat requires namespace kagent")
		}
		return "kagent", "kagent", nil
	}
	if opt.Runtime == "orka" {
		namespace := opt.Namespace
		if namespace == "" {
			namespace = OrkaNamespace
		}
		return "orka", namespace, nil
	}
	ref, err := resolveRegisteredRuntime(a.operationContext(), a.chatRuntimes(), agentruntime.Target{Context: a.Cfg.KubeContext, Namespace: opt.Namespace, Name: name})
	if err != nil {
		return "", "", err
	}
	return string(ref.Runtime), ref.Namespace, nil
}
