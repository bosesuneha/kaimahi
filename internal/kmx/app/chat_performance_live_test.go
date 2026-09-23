package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

// Explicit opt-in: by default runs three native short-answer Tasks or three
// Copilot read-only pod-list turns. KMX_PROFILE_SCENARIO=tools selects native
// pod-list Tasks. Outputs timings/counts, never prompts, keys or replies.
func TestLiveChatPerformance(t *testing.T) {
	mode := os.Getenv("KMX_PROFILE_MODE")
	if mode == "" {
		t.Skip("set KMX_PROFILE_MODE=native|copilot and KMX_PROFILE_CONTEXT")
	}
	if mode != "native" && mode != "copilot" && mode != "foundry" {
		t.Fatal("unknown profile mode")
	}
	kubeContext := os.Getenv("KMX_PROFILE_CONTEXT")
	if kubeContext == "" {
		t.Fatal("explicit context required")
	}
	agent := os.Getenv("KMX_PROFILE_AGENT")
	if agent == "" {
		agent = "hello-world-agent"
	}
	a := &App{Cfg: &config.Config{KubeContext: kubeContext}, Run: &run.Runner{}, Out: io.Discard, Err: io.Discard}
	b := &orkaChatBackend{app: a, agent: agent, namespace: OrkaNamespace}
	defer b.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	b.chatContext = ctx
	if mode == "foundry" {
		c := foundryChatConfig{Endpoint: os.Getenv("KMX_PROFILE_FOUNDRY_ENDPOINT"), Deployment: os.Getenv("KMX_PROFILE_FOUNDRY_DEPLOYMENT"), Tenant: os.Getenv("KMX_PROFILE_FOUNDRY_TENANT"), Audience: os.Getenv("KMX_PROFILE_FOUNDRY_AUDIENCE")}
		client, err := newFoundryChatClient(c)
		if err != nil {
			t.Fatal(err)
		}
		a.foundryClient = client
		defer client.http.CloseIdleConnections()
		for i := 0; i < 3; i++ {
			start := time.Now()
			raw, err := a.orkaCapture(ctx, nil, "-n", OrkaNamespace, "get", "agents.core.orka.ai", agent, "-o", "json")
			if err != nil {
				t.Fatal(err)
			}
			instructions, err := b.copilotInstructionsFromAgent(ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			tools, _, err := b.copilotToolsFromAgent(ctx, raw)
			if err != nil {
				t.Fatal(err)
			}
			lookup := time.Since(start)
			r := newChatRenderer(io.Discard)
			r.verbose = true
			r.timeline = func(event chatTimelineEvent) {
				if event.label == "TIMING" {
					t.Log(event.text)
				}
			}
			answer, err := b.foundryTurn(ctx, instructions, "what pods are running?", tools, r)
			valid := strings.Contains(answer, "orka-controller-manager") && strings.Contains(answer, "kmx-k8s-tool")
			t.Logf("run=%d total=%s lookup=%s answer_shape_valid=%t error=%v", i+1, time.Since(start), lookup, valid, err)
			if err != nil || !valid {
				t.Fatal("Foundry tool benchmark failed")
			}
		}
		return
	}
	runs := 3
	if value := os.Getenv("KMX_PROFILE_RUNS"); value != "" {
		var err error
		runs, err = strconv.Atoi(value)
		if err != nil || runs < 1 || runs > 10 {
			t.Fatal("KMX_PROFILE_RUNS must be 1..10")
		}
	}
	if mode == "native" {
		start := time.Now()
		session, err := a.openOrkaResultSession(ctx, CreateOptions{Namespace: OrkaNamespace, ResultServiceAccount: "orka-result-reader", OrkaAPIService: "orka-api", ResultPort: "19189"})
		if err != nil {
			t.Fatal(err)
		}
		defer session.close()
		t.Logf("session_setup=%s", time.Since(start))
		prompt := "Reply with exactly: ready. Do not call tools."
		toolScenario := os.Getenv("KMX_PROFILE_SCENARIO") == "tools"
		if toolScenario {
			prompt = "what pods are running?"
		}
		for i := 0; i < runs; i++ {
			p := &orkaTaskProfile{}
			start = time.Now()
			answer, err := a.runQuickstartOrkaTaskProfile(ctx, agent, OrkaNamespace, prompt, p, nil, session)
			if err != nil {
				t.Fatal(err)
			}
			valid := strings.EqualFold(strings.Trim(strings.TrimSpace(answer), ".\"`!"), "ready")
			if toolScenario {
				valid = strings.Contains(answer, "orka-controller-manager") && strings.Contains(answer, "kmx-k8s-tool")
			}
			t.Logf("run=%d total=%s answer_shape_valid=%t\n%s", i+1, time.Since(start), valid, p.summary())
			if !valid {
				t.Errorf("run %d failed answer shape validation", i+1)
			}
		}
		return
	}
	a.copilotCLI = detectCopilotCLI()
	a.copilotModel = os.Getenv("KMX_PROFILE_MODEL")
	if a.copilotModel == "" {
		a.copilotModel = "auto"
	}
	for i := 0; i < runs; i++ {
		start := time.Now()
		raw, err := a.orkaCapture(ctx, nil, "-n", OrkaNamespace, "get", "agents.core.orka.ai", agent, "-o", "json")
		if err != nil {
			t.Fatal(err)
		}
		instructions, err := b.copilotInstructionsFromAgent(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		tools, _, err := b.copilotToolsFromAgent(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		lookup := time.Since(start)
		calls, executions := 0, 0
		var processes, toolTime time.Duration
		answer, err := copilotToolLoop(ctx, instructions, "what pods are running?", tools, func(ctx context.Context, prompt string) (string, error) {
			calls++
			s := time.Now()
			reply, err := copilotPromptModel(ctx, a.copilotCLI, a.copilotModel, prompt)
			elapsed := time.Since(s)
			processes += elapsed
			t.Logf("run=%d process=%d duration=%s prompt_bytes=%d", i+1, calls, elapsed, len(prompt))
			return reply, err
		}, func(ctx context.Context, tool copilotTool, args []byte) (string, error) {
			// Refuse any model-selected operation beyond the benchmark's read-only scope.
			var input map[string]any
			if tool.Name != "k8s-get-resources" || json.Unmarshal(args, &input) != nil || input["resource"] != "pods" {
				return "", fmt.Errorf("benchmark only allows pod listing")
			}
			executions++
			s := time.Now()
			result, err := b.executeCopilotTool(ctx, tool, args)
			toolTime += time.Since(s)
			return string(result), err
		})
		t.Logf("run=%d total=%s lookup=%s processes=%s tools=%s calls=%d executions=%d nonempty=%t error=%v", i+1, time.Since(start), lookup, processes, toolTime, calls, executions, strings.TrimSpace(answer) != "", err)
		if err != nil || executions == 0 {
			t.Errorf("tool workflow failed on run %d", i+1)
		}
	}
}
