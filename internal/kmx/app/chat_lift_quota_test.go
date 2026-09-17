package app

import (
	"encoding/json"
	"testing"
)

func TestFoundryQuotaChoicesOnlyOfferConfirmedCapacity(t *testing.T) {
	var models []foundryModel
	err := json.Unmarshal([]byte(`[
 {"name":"chat","version":"v1","format":"OpenAI","capabilities":{"chatCompletion":"true"},"skus":[
  {"name":"Standard","usageName":"standard-chat","capacity":{"minimum":2}},
  {"name":"GlobalStandard","usageName":"global-chat","capacity":{"minimum":1}},
  {"name":"UnknownSKU","usageName":"missing"}]},
 {"name":"full","format":"OpenAI","capabilities":{"chatCompletion":"true"},"skus":[{"name":"Standard","usageName":"full"}]},
 {"name":"blocked","format":"OpenAI","capabilities":{"chatCompletion":"true"},"skus":[{"name":"Standard","usageName":"blocked"}]},
 {"name":"deprecated","format":"OpenAI","lifecycleStatus":"Deprecated","capabilities":{"chatCompletion":"true"},"skus":[{"name":"Standard","usageName":"global-chat"}]}
]`), &models)
	if err != nil {
		t.Fatal(err)
	}
	var usages []foundryUsage
	err = json.Unmarshal([]byte(`[
 {"name":{"value":"standard-chat"},"limit":10,"currentValue":8},
 {"name":{"value":"GLOBAL-CHAT"},"limit":10,"currentValue":9},
 {"name":{"value":"full"},"limit":10,"currentValue":10},
 {"name":{"value":"blocked"},"limit":10,"currentValue":0,"status":"Blocked"}
]`), &usages)
	if err != nil {
		t.Fatal(err)
	}
	choices := quotaChoices(models, usages)
	if len(choices) != 2 || choices[0].SKU.Name != "GlobalStandard" || choices[0].Capacity != 1 || choices[0].Remaining != 1 || choices[1].Capacity != 2 {
		t.Fatalf("choices=%+v", choices)
	}
	if got := quotaChoices(models, nil); len(got) != 0 {
		t.Fatalf("unconfirmed quota offered: %+v", got)
	}
	usages[0].CurrentValue = nil
	usages[1].Limit = nil
	if got := quotaChoices(models, usages); len(got) != 0 {
		t.Fatalf("missing usage data treated as zero: %+v", got)
	}
}

func TestFoundryRegionalCatalogUnwrapsModels(t *testing.T) {
	models, err := parseFoundryCatalog([]byte(`[{"kind":"AIServices","model":{"name":"chat","format":"OpenAI","version":"v1","skus":[{"name":"Standard","usageName":"quota-chat"}]}}]`), true)
	if err != nil || len(models) != 1 || models[0].Name != "chat" || models[0].SKUs[0].UsageName != "quota-chat" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
}

func TestQuotaScopeAndDefaultVersionRecommendation(t *testing.T) {
	raw := []byte(`[{"model":{"format":"OpenAI","name":"gpt-4.1-mini","version":"2025-04-14","isDefaultVersion":true,"capabilities":{"chatCompletion":"true"},"skus":[{"name":"GlobalStandard","usageName":"OpenAI.GlobalStandard.gpt-4.1-mini","capacity":{"minimum":null,"step":null}}]}},{"model":{"format":"OpenAI","name":"gpt-4.1-mini","version":"2025-04-14","capabilities":{"chatCompletion":"true"},"skus":[{"name":"GlobalStandard","usageName":"OpenAI.GlobalStandard.gpt-4.1-mini"}]}}]`)
	models, err := parseFoundryCatalog(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	var usage []foundryUsage
	if err := json.Unmarshal([]byte(`[{"name":{"value":"OpenAI.GlobalStandard.gpt-4.1-mini"},"limit":1000,"currentValue":50,"scopeType":"Global","scopeId":"global","status":null}]`), &usage); err != nil {
		t.Fatal(err)
	}
	choices := quotaChoices(models, usage)
	if len(choices) != 1 || choices[0].QuotaScope != "Global/global" || choices[0].Remaining != 950 || choices[0].Capacity != 1 {
		t.Fatalf("choices=%+v", choices)
	}
}

func TestFoundryCapacityRespectsStepsAndAllowedValues(t *testing.T) {
	sku := foundrySKU{}
	sku.Capacity.Step = 10
	if got := foundryMinimumCapacity(sku); got != 10 {
		t.Fatalf("capacity=%d", got)
	}
	sku.Capacity.AllowedValues = []int{50, 20, 100}
	sku.Capacity.Minimum = 25
	if got := foundryMinimumCapacity(sku); got != 50 {
		t.Fatalf("capacity=%d", got)
	}
	sku.Capacity.Maximum = 40
	if got := foundryMinimumCapacity(sku); got != 0 {
		t.Fatalf("invalid capacity=%d", got)
	}
}

func TestFoundryEmptyCatalogIsNotReportedAsQuotaShortage(t *testing.T) {
	reason := foundryAvailabilityReason([]foundryModel{{Name: "Cohere", Format: "Cohere"}}, "westus2")
	if reason != "No OpenAI chat models offered in westus2; subscription quota alone is not regional availability" {
		t.Fatalf("reason=%s", reason)
	}
}
