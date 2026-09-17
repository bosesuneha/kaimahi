package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/cliui"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/guard"
	"golang.org/x/term"
)

// The terminal scroll region keeps transcript output below the status header.
// The raw editor uses the same reserved row count when sizing its viewport.
func (r *chatRenderer) drawStickyHeaderLocked() {
	file, ok := r.out.(*os.File)
	if !ok || !r.stickyHeader || !r.alternateScreen {
		return
	}
	width, height, err := term.GetSize(int(file.Fd()))
	if err != nil || width < 8 || height < 8 {
		return
	}
	fields := map[string]string{}
	for _, field := range r.headerFields {
		fields[field.Label] = field.Value
	}
	location := fields["Location"]
	if location == "" {
		location = r.headerContext
	}
	lines := []string{
		"KMX / " + tuiField("agent", r.headerAgent) + " · " + tuiField("location", location),
		fields["Inference"],
		fields["Tools"],
	}
	if lines[1] == "" {
		lines[1] = fields["Runtime"]
	}
	if lines[2] == "" {
		lines[2] = "/help · /tools · /agent · /exit"
	} else {
		lines[2] = tuiField("tools", " "+lines[2])
	}
	lines[1] = tuiField("inference", " "+lines[1])
	// Save cursor while refreshing a header over an existing conversation.
	fmt.Fprint(r.out, "\x1b7\x1b[r\x1b[H")
	for i, line := range lines {
		if i > 0 {
			fmt.Fprint(r.out, "\r\n")
		}
		fmt.Fprint(r.out, "\x1b[2K", ansi.Truncate(line, max(1, width-1), "…"))
	}
	fmt.Fprint(r.out, "\r\n\x1b[2K", r.ui.Muted(strings.Repeat("─", max(1, width-1))))
	fmt.Fprintf(r.out, "\x1b[5;%dr", height)
	if r.headerRows == 0 {
		fmt.Fprint(r.out, "\x1b[5;1H")
	} else {
		fmt.Fprint(r.out, "\x1b8")
	}
	r.headerRows = 4
}

// Classify the actual kubeconfig target, not just its display name: a remote
// context can also be named kind-*. Inference running on the host does not
// change the location of the connected Agent resource.
func (a *App) chatLocation(ctx context.Context) (string, error) {
	raw, err := a.orkaCapture(ctx, nil, "config", "view", "-o", "json")
	if err != nil {
		return "", err
	}
	cfg, err := guard.ParseKubeconfig(raw)
	if err != nil {
		return "", err
	}
	posture, err := guard.Classify(cfg, a.Cfg.KubeContext)
	if err != nil {
		return "", err
	}
	if posture.Local {
		return "local-" + a.Cfg.KubeContext, nil
	}
	return "remote-" + displayClusterName(a.Cfg.KubeContext, a.chatClusterName), nil
}

func (r *chatRenderer) suspendStickyHeader() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearLocked()
	r.closeLocked()
	if r.headerRows > 0 {
		fmt.Fprint(r.out, "\x1b[r")
		r.headerRows = 0
	}
}

func (r *chatRenderer) updateHeaderFieldLocked(label, payload string) {
	for i := range r.headerFields {
		if r.headerFields[i].Label == label {
			r.headerFields[i].Value = payload
			return
		}
	}
	r.headerFields = append(r.headerFields, cliui.Field{Label: label, Value: payload})
}
