package selfupdate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// scripted answers each check with the next result in turn.
type scripted struct {
	mu      sync.Mutex
	results []*Release
	errs    []error
	calls   int
}

func (s *scripted) check() (*Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.calls
	s.calls++
	if i >= len(s.results) {
		i = len(s.results) - 1
	}
	return s.results[i], s.errs[i]
}

func (s *scripted) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestCheckNowReportsOnlyChanges(t *testing.T) {
	v2 := &Release{Version: "0.2.0", ReleaseURL: "https://example.com/v0.2.0"}
	v3 := &Release{Version: "0.3.0", ReleaseURL: "https://example.com/v0.3.0"}
	s := &scripted{
		results: []*Release{v2, {Version: "0.2.0", ReleaseURL: "https://example.com/v0.2.0"}, nil, v3, nil},
		errs:    []error{nil, nil, errors.New("offline"), nil, nil},
	}
	var changes []*Release
	w := &Watcher{Check: s.check, Changed: func(r *Release) { changes = append(changes, r) }}

	for range 5 {
		_, _ = w.CheckNow()
	}

	want := []*Release{v2, v3, nil}
	if len(changes) != len(want) {
		t.Fatalf("changes = %v, want %v (the same release again, and a failed check, change nothing)", changes, want)
	}
	for i := range want {
		if !sameRelease(changes[i], want[i]) {
			t.Errorf("change %d = %v, want %v", i, changes[i], want[i])
		}
	}
	if w.Latest() != nil {
		t.Errorf("latest = %v after an up-to-date check", w.Latest())
	}
}

func TestAFailedCheckKeepsTheLastResult(t *testing.T) {
	v2 := &Release{Version: "0.2.0"}
	s := &scripted{results: []*Release{v2, nil}, errs: []error{nil, errors.New("rate limited")}}
	w := &Watcher{Check: s.check}

	_, _ = w.CheckNow()
	if _, err := w.CheckNow(); err == nil {
		t.Fatal("the failed check's error was lost")
	}
	if w.Latest() != v2 {
		t.Errorf("latest = %v, want the release found before the failure", w.Latest())
	}
}

func TestRunChecksAfterTheDelayThenOnSchedule(t *testing.T) {
	s := &scripted{results: []*Release{{Version: "0.2.0"}, nil}, errs: []error{nil, errors.New("offline")}}
	failed := make(chan error, 10)
	w := &Watcher{Check: s.check, Failed: func(err error) { failed <- err }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx, 50*time.Millisecond, 20*time.Millisecond); close(done) }()

	time.Sleep(25 * time.Millisecond)
	if n := s.count(); n != 0 {
		t.Errorf("%d checks before the first delay", n)
	}
	select {
	case <-failed: // the second check, on schedule
	case <-time.After(2 * time.Second):
		t.Fatal("no scheduled check after the first")
	}
	if w.Latest() == nil || w.Latest().Version != "0.2.0" {
		t.Errorf("latest = %v, want the first check's release kept", w.Latest())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run didn't stop when its context ended")
	}
}
