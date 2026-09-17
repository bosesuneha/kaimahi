package app

import (
	"context"
	"encoding/json"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFoundryReadinessRequiresSuccess(t *testing.T) {
	for _, tc := range []struct {
		state       string
		ready, fail bool
	}{{"Succeeded", true, false}, {"Creating", false, false}, {"Failed", false, true}, {"Canceled", false, true}, {"", false, true}} {
		raw, _ := json.Marshal(map[string]any{"properties": map[string]string{"provisioningState": tc.state}})
		ready, err := foundryResourceState(raw)
		if ready != tc.ready || (err != nil) != tc.fail {
			t.Fatalf("%+v ready=%v err=%v", tc, ready, err)
		}
	}
}

func TestFoundryProbeUsesSecretReferenceAndSelectedEndpoint(t *testing.T) {
	job := foundryProbeJob("probe", "target-ns", "target-key", "https://foundry.openai.azure.com/openai/v1", "selected-model")
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"secretName":"target-key"`, `"namespace":"target-ns"`, `"value":"selected-model"`, `"value":"https://foundry.openai.azure.com/openai/v1"`, `"automountServiceAccountToken":false`, `"backoffLimit":0`, `"activeDeadlineSeconds":120`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(string(raw), `"stringData"`) {
		t.Fatal("credential value embedded in probe")
	}
}

func TestFoundryEndpointProbeWaitsAndCleansOnlyProbeJob(t *testing.T) {
	for _, outcome := range []string{"succeeded", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls")
			body := filepath.Join(dir, "body")
			t.Setenv("PROBE_LOG", log)
			t.Setenv("PROBE_BODY", body)
			t.Setenv("PROBE_STATE", outcome)
			fakeTool(t, dir, "kubectl", `printf '%s\n' "$*" >> "$PROBE_LOG"
case "$*" in
 *"create -f -"*) /bin/cat > "$PROBE_BODY" ;;
 *"get job"*) printf '{"status":{"%s":1}}' "$PROBE_STATE" ;;
 *"delete job"*) exit 0 ;;
 *) exit 1 ;;
esac`)
			t.Setenv("PATH", dir)
			a := &App{Cfg: &config.Config{KubeContext: "remote-aks"}, Run: &run.Runner{}, Err: io.Discard}
			err := a.verifyFoundryEndpoint(context.Background(), "target-ns", "target-key", "https://example.openai.azure.com/openai/v1", "model-deployment")
			if (err != nil) != (outcome == "failed") {
				t.Fatalf("err=%v", err)
			}
			calls, _ := os.ReadFile(log)
			for _, want := range []string{"--context remote-aks", "-n target-ns create", "get job kmx-inference-check-", "delete job kmx-inference-check-"} {
				if !strings.Contains(string(calls), want) {
					t.Fatalf("missing %q: %s", want, calls)
				}
			}
			if strings.Contains(string(calls), "delete secret") {
				t.Fatal("credential Secret removed")
			}
			raw, _ := os.ReadFile(body)
			if !strings.Contains(string(raw), `"secretName":"target-key"`) {
				t.Fatal("probe not wired to target key")
			}
		})
	}
}
