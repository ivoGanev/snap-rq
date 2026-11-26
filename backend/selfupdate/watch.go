package selfupdate

import (
	"context"
	"sync"
	"time"
)

// Release is a newer version than the running one.
type Release struct {
	Version string `json:"version"`
	// ReleaseURL is the release's page, to download it by hand.
	ReleaseURL string `json:"release_url"`
	// Blocked explains why this install can't update itself; empty when it can.
	Blocked string `json:"blocked,omitempty"`
}

// Watcher keeps the newest release found, from scheduled checks and checks
// asked for, and reports when it changes.
type Watcher struct {
	// Check asks for a newer release; nil when up to date.
	Check func() (*Release, error)
	// Changed is told the new result (nil: up to date) whenever it differs.
	Changed func(*Release)
	// Failed is told a scheduled check's error; the last result stays.
	Failed func(error)

	mu     sync.Mutex
	latest *Release
}

// Latest is the newest release found so far, or nil.
func (w *Watcher) Latest() *Release {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.latest
}

// CheckNow checks once and keeps the result.
func (w *Watcher) CheckNow() (*Release, error) {
	release, err := w.Check()
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	changed := !sameRelease(w.latest, release)
	w.latest = release
	w.mu.Unlock()
	if changed && w.Changed != nil {
		w.Changed(release)
	}
	return release, nil
}

// Run checks after first, then every interval, until ctx is done.
func (w *Watcher) Run(ctx context.Context, first, every time.Duration) {
	wait := first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if _, err := w.CheckNow(); err != nil && w.Failed != nil {
			w.Failed(err)
		}
		wait = every
	}
}

func sameRelease(a, b *Release) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
