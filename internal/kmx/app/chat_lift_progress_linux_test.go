package app

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestLiftFetchSpinnerClearsBeforeNextPane(t *testing.T) {
	master, slave := chatPTY(t, 80)
	done := make(chan error, 1)
	go func() {
		_, err := liftFetchProgress(t.Context(), slave, "Fetching subscriptions for tenant:test", func(context.Context) ([]byte, error) { time.Sleep(250 * time.Millisecond); return []byte("ok"), nil })
		done <- err
	}()
	var captured strings.Builder
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Count(s, "Fetching subscriptions") >= 2 })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(slave, "NEXT PANE\n")
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.HasSuffix(s, "NEXT PANE\r\n") })
	if !strings.Contains(captured.String(), "\r\x1b[2KNEXT PANE") {
		t.Fatalf("spinner not cleared: %q", captured.String())
	}
}
