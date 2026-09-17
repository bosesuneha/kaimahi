package app

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func tuiField(key, value string) string {
	key = strings.Join(strings.Fields(safeTerminal(key)), " ")
	space := ""
	if strings.HasPrefix(value, " ") {
		space = " "
	}
	value = strings.Join(strings.Fields(safeTerminal(value)), " ")
	return lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Render(key+":"+space) +
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Cyan).Render(value)
}

// Only known renderer-owned labels are styled as fields. Colons within an
// endpoint or free-form instructions are not interpreted as new labels.
func tuiDetailLine(text string) string {
	parts := strings.Split(safeTerminal(text), " · ")
	for i, part := range parts {
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "agent", "source", "target", "location", "from", "to", "server", "subscription", "resource group", "rg", "aks", "account", "region", "model", "endpoint", "sku", "capacity", "secret", "tools", "output", "search", "elapsed":
			parts[i] = tuiField(key, " "+strings.TrimSpace(value))
		}
	}
	return strings.Join(parts, " · ")
}
