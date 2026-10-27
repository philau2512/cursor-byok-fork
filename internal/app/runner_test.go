package app

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdRefreshCoalescerCancelsAndCoalescesFocusTriggers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	var calls atomic.Int32
	trigger := newAdRefreshCoalescer(ctx, func(ctx context.Context) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		finished <- struct{}{}
	})

	trigger()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	for index := 0; index < 32; index++ {
		trigger()
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("first refresh did not finish")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("coalesced refresh did not run")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("coalesced refresh did not finish")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("refresh calls = %d, want 2", got)
	}

	cancel()
	trigger()
	time.Sleep(25 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Fatalf("refresh ran after cancellation: calls = %d, want 2", got)
	}
}

func TestWindowsAdditionalBrowserArgs(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{name: "unset"},
		{name: "enabled with one", env: "1", want: []string{"--no-sandbox"}},
		{name: "enabled with true", env: " true ", want: []string{"--no-sandbox"}},
		{name: "disabled", env: "false"},
		{name: "invalid", env: "yes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(disableWebViewSandboxEnv, tt.env)
			if got := windowsAdditionalBrowserArgs(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("windowsAdditionalBrowserArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}
