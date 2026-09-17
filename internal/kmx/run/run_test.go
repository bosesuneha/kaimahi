package run

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunnerContextCancelsActiveCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{Context: ctx}
	done := make(chan error, 1)
	go func() { done <- r.Run("sleep", "30") }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled command error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled command remained active")
	}
}

// Poll is bounded by ATTEMPTS, not by wall-clock — the shell's
// `for _ in $(seq 1 N)`. The difference bites exactly where it matters: each
// check is a real API call, and on a slow path (kind on podman inside a VM)
// those calls are what gets slower, so a wall-clock budget would quietly turn
// "120 tries" into "however few tries fit in 120 seconds" on the machines
// that need the wait to be longest.
func TestPollRunsEveryAttemptHoweverSlowTheCheckIs(t *testing.T) {
	calls := 0
	slow := func() bool {
		calls++
		time.Sleep(5 * time.Millisecond) // each check outlives the interval
		return false
	}
	if Poll(6, time.Millisecond, slow) {
		t.Error("a check that never succeeds must not report success")
	}
	if calls != 6 {
		t.Errorf("check ran %d times, want 6 — the budget is attempts, not elapsed time", calls)
	}
}

func TestPollStopsAtTheFirstSuccess(t *testing.T) {
	calls := 0
	if !Poll(10, time.Millisecond, func() bool { calls++; return calls == 3 }) {
		t.Error("want success on the third try")
	}
	if calls != 3 {
		t.Errorf("check ran %d times after succeeding on the third", calls)
	}
}

// A zero-attempt poll is a poll that never checked; it must not read as
// success.
func TestPollWithNoAttemptsFails(t *testing.T) {
	if Poll(0, time.Millisecond, func() bool { return true }) {
		t.Error("zero attempts must not report success")
	}
}

func TestPollContextCancelsRetryDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checked := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- PollContext(ctx, 60, time.Hour, func() bool {
			close(checked) // A second check would panic.
			return false
		})
	}()
	<-checked
	cancel()
	select {
	case ready := <-done:
		if ready {
			t.Fatal("cancelled poll reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt retry delay")
	}
}
