package app

import (
	"strings"
	"testing"
)

func TestOrkaEnabledToolsSummaryCountsEffectiveTools(t *testing.T) {
	summary, err := orkaEnabledToolsSummary([]byte(`{"spec":{"tools":[{"name":"k8s-get-resources"},{"name":"disabled","enabled":false},{"name":"remember","enabled":false},{"name":"k8s-get-resources","enabled":true}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "(5 tools enabled) k8s-get-resources, propose_memory, recall_memory, remember, search_transcript"
	if summary != want {
		t.Fatalf("summary=%q want=%q", summary, want)
	}
	summary, err = orkaEnabledToolsSummary([]byte(`{"spec":{}}`))
	if err != nil || !strings.HasPrefix(summary, "(4 tools enabled)") {
		t.Fatalf("summary=%q err=%v", summary, err)
	}
	if _, err := orkaEnabledToolsSummary([]byte(`invalid`)); err == nil {
		t.Fatal("invalid Agent accepted")
	}
}
