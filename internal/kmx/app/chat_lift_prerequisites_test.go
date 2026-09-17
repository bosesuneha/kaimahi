package app

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

func TestLiftCRDCheckDistinguishesMissingFromPermissionFailure(t *testing.T) {
	for _, fails := range []bool{false, true} {
		dir := t.TempDir()
		script := "exit 0"
		if fails {
			script = "exit 1"
		}
		fakeTool(t, dir, "kubectl", script)
		t.Setenv("PATH", dir)
		a := &App{Cfg: &config.Config{KubeContext: "aks-target"}, Run: &run.Runner{}}
		missing, err := a.missingLiftOrkaCRDs(context.Background())
		if fails {
			if err == nil || !strings.Contains(err.Error(), "aks-target") {
				t.Fatalf("err=%v", err)
			}
		} else if err != nil || len(missing) != 4 {
			t.Fatalf("missing=%v err=%v", missing, err)
		}
	}
}

func TestFoundryCommandsPinScopeModelAndCapacity(t *testing.T) {
	cluster := chatLiftTarget{Subscription: "selected-sub", ResourceGroup: "aks-rg", Location: "eastus"}
	account := foundryAccount{Name: "foundry", ResourceGroup: cluster.ResourceGroup, Location: cluster.Location}
	args := foundryCreateArgs(cluster, account)
	want := []string{"cognitiveservices", "account", "create", "--subscription", "selected-sub", "--resource-group", "aks-rg", "--name", "foundry", "--kind", "AIServices", "--sku", "S0", "--location", "eastus", "--custom-domain", "foundry", "--yes", "-o", "json", "--only-show-errors"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args=%v", args)
	}
	args = foundryDeploymentArgs(cluster, account, "deployment", foundryModel{Name: "gpt-model", Version: "version-1", Format: "OpenAI"}, "GlobalStandard", 1)
	for _, pair := range []string{"--subscription selected-sub", "--resource-group aks-rg", "--model-version version-1", "--model-name gpt-model", "--sku-name GlobalStandard", "--sku-capacity 1"} {
		if !strings.Contains(strings.Join(args, " "), pair) {
			t.Fatalf("missing %s in %v", pair, args)
		}
	}
}

func TestFoundryEndpointUsesOpenAIV1(t *testing.T) {
	account := foundryAccount{}
	account.Properties.CustomSubDomainName = "my-foundry"
	got, err := foundryBaseURL(account)
	if err != nil || got != "https://my-foundry.openai.azure.com/openai/v1" {
		t.Fatalf("url=%q err=%v", got, err)
	}
	account.Properties.CustomSubDomainName = ""
	account.Properties.Endpoint = "http://untrusted"
	if _, err := foundryBaseURL(account); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
}

func TestLiftInferenceRebindsTargetSecretWithoutChangingAgent(t *testing.T) {
	bundle, err := createOrkaBundle(CreateOptions{Name: "demo", Namespace: OrkaNamespace, ProviderType: "openai", Model: "local", Secret: "source-key"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(bundle.Agent)
	selected := map[string]any{"type": "openai", "defaultModel": "remote-deployment", "baseURL": "https://example.openai.azure.com/openai/v1", "secretRef": map[string]any{"name": "target-key", "key": "api-key"}}
	if err := useLiftProvider(bundle, selected); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(bundle.Agent)
	if string(before) != string(after) || bundle.Secret["metadata"].(map[string]any)["name"] != "target-key" {
		t.Fatal("Agent changed or Secret reference stale")
	}
	selected["defaultModel"] = "changed"
	if bundle.Provider["spec"].(map[string]any)["defaultModel"] != "remote-deployment" {
		t.Fatal("selected configuration aliased")
	}
}
