package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuickstartInferenceNeverAutomaticallySelects(t *testing.T) {
	for _, tc := range []struct {
		mode, path, want string
		fail             bool
	}{
		{"auto", "/bin/copilot", "", false},
		{"", "/bin/copilot", "", false},
		{"auto", "", "", false},
		{"local", "/bin/copilot", "local", false},
		{"copilot", "", "", true},
		{"other", "", "", true},
	} {
		got, err := quickstartInference(tc.mode, tc.path)
		if got != tc.want || (err != nil) != tc.fail {
			t.Fatalf("%+v: got=%q err=%v", tc, got, err)
		}
	}
}

func TestCopilotDetectionOnlyChecksExecutable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if got := detectCopilotCLI(); got != "" {
		t.Fatalf("unexpected CLI: %s", got)
	}
	fakeTool(t, dir, "copilot", "exit 99")
	if got := detectCopilotCLI(); got != filepath.Join(dir, "copilot") {
		t.Fatalf("CLI=%q", got)
	}
}

func TestCopilotPromptUsesAutoWithoutShellOrWorkspace(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	t.Setenv("KMX_COPILOT_TEST_ARGS", argsFile)
	fakeTool(t, dir, "copilot", `printf '%s\n' "$PWD" "$@" > "$KMX_COPILOT_TEST_ARGS"
printf '%s\n' '{"type":"assistant.message","data":{"content":"hello"}}'`)
	message := "hi; $(touch should-not-exist)"
	answer, err := copilotPrompt(t.Context(), filepath.Join(dir, "copilot"), "Agent instructions", message)
	if err != nil || answer != "hello" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"-p\nAgent instructions", message, "--model\nauto\n", "--available-tools=kmx_tool_adapter_only\n", "--no-custom-instructions\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "--no-auto-login") {
		t.Fatal("wrapper disabled normal credential resolution")
	}
	workdir, _, _ := strings.Cut(text, "\n")
	if !strings.Contains(filepath.Base(workdir), "kmx-copilot-") {
		t.Fatalf("unexpected workdir %s", workdir)
	}
	if _, err := os.Stat(workdir); !os.IsNotExist(err) {
		t.Fatalf("temporary directory retained: %v", err)
	}
}

func TestCopilotPromptFailureAndCancellation(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "copilot", "exit 1")
	if _, err := copilotPrompt(t.Context(), filepath.Join(dir, "copilot"), "", "hello"); err == nil || !strings.Contains(err.Error(), "copilot login") {
		t.Fatalf("err=%v", err)
	}
	fakeTool(t, dir, "copilot", "exec sleep 30")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := copilotPrompt(ctx, filepath.Join(dir, "copilot"), "", "hello"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Copilot did not stop promptly")
	}
}

func TestCopilotLoginStatusUsesCLIProtocol(t *testing.T) {
	for _, tc := range []struct{ response, want string }{
		{`{"jsonrpc":"2.0","id":1,"result":{"isAuthenticated":true,"authType":"gh-cli"}}`, "logged in"},
		{`{"jsonrpc":"2.0","id":1,"result":{"isAuthenticated":false}}`, "not logged in"},
		{`{"jsonrpc":"2.0","id":1,"error":{"code":-32601}}`, "status unavailable"},
	} {
		dir := t.TempDir()
		frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(tc.response), tc.response)
		fakeTool(t, dir, "copilot", "printf '%s' "+shellArg(frame))
		if got := copilotLoginStatus(t.Context(), filepath.Join(dir, "copilot")); got != tc.want {
			t.Fatalf("got=%q want=%q", got, tc.want)
		}
	}
}

func TestCopilotLoginStatusCancellation(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "copilot", "exec sleep 30")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if got := copilotLoginStatus(ctx, filepath.Join(dir, "copilot")); got != "status unavailable" {
		t.Fatalf("got=%q", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("auth probe ignored cancellation")
	}
}
