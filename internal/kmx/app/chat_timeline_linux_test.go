package app

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/cliui"
	"golang.org/x/sys/unix"
)

type timelineWaitingBackend struct {
	started chan string
	stopped chan struct{}
}

func (b *timelineWaitingBackend) Agent() string { return "demo" }
func (b *timelineWaitingBackend) Connect(context.Context, *chatRenderer) ([]cliui.Field, error) {
	return []cliui.Field{{Label: "Location", Value: "kind-test"}}, nil
}
func (b *timelineWaitingBackend) Send(ctx context.Context, message string, r *chatRenderer) error {
	b.started <- message
	r.beginAssistant("demo")
	r.assistantOperation("demo", "TOOL CALL", "k8s-get-resources", colorBlue, "Waiting for tool")
	<-ctx.Done()
	close(b.stopped)
	return ctx.Err()
}

func TestTimelinePTYEditsAndCancelsRunningResponse(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	master, slave := chatPTY(t, 80)
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	b := &timelineWaitingBackend{started: make(chan string, 1), stopped: make(chan struct{})}
	a := &App{Stdin: slave, Out: slave, Err: slave}
	done := make(chan error, 1)
	go func() { done <- a.runInteractiveChatBackend(b) }()
	var captured strings.Builder
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Contains(ansi.Strip(s), "Enter send") })
	_, _ = io.WriteString(master, "hello world\x17\x01X\x05!\r")
	select {
	case message := <-b.started:
		if message != "Xhello !" {
			t.Fatalf("edited message=%q", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("edited message not sent")
	}
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Contains(ansi.Strip(s), "TOOL CALL") })
	_, _ = io.WriteString(master, "\x03")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("response ignored Ctrl-C")
	}
	select {
	case <-b.stopped:
	default:
		t.Fatal("worker still active")
	}
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *before != *after {
		t.Fatalf("terminal not restored: %v", err)
	}
}
