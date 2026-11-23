package commands

import (
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// waitForCancel runs until its run is cancelled, then finishes twice; the
// second Finish must not reach the frontend.
type waitForCancel struct{ started chan struct{} }

func (waitForCancel) Info() Info { return Info{FunctionID: "test.wait"} }

func (c waitForCancel) Run(_ json.RawMessage, obs Observer) {
	c.started <- struct{}{}
	<-obs.Context.Done()
	obs.Finish(nil, obs.Context.Err())
	obs.Finish("late", nil)
}

type panics struct{}

func (panics) Info() Info                    { return Info{FunctionID: "test.panic"} }
func (panics) Run(json.RawMessage, Observer) { panic("boom") }

// watchFinishes records every command:finish the runner emits.
func watchFinishes(t *testing.T) (finishes func() []Finish, wait func(n int)) {
	t.Helper()
	var mu sync.Mutex
	var got []Finish
	original := emit
	emit = func(events []event) {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range events {
			if f, ok := e.data.(Finish); ok && e.name == EventFinish {
				got = append(got, f)
			}
		}
	}
	t.Cleanup(func() { emit = original })
	finishes = func() []Finish {
		mu.Lock()
		defer mu.Unlock()
		return append([]Finish(nil), got...)
	}
	wait = func(n int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for len(finishes()) < n {
			if time.Now().After(deadline) {
				t.Fatalf("%d finishes, want %d", len(finishes()), n)
			}
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return finishes, wait
}

func TestCancelEndsARunWithOneFinish(t *testing.T) {
	r, _ := newTestRunner(t)
	finishes, wait := watchFinishes(t)
	started := make(chan struct{}, 1)
	if err := r.RegisterObservable(func(*sql.DB) ObservableCommand { return waitForCancel{started} }); err != nil {
		t.Fatal(err)
	}

	runID, err := r.Begin("test.wait", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	r.Cancel(runID)
	wait(1)

	got := finishes()
	if len(got) != 1 || got[0].RunID != runID || got[0].Error == "" {
		t.Errorf("finishes = %+v, want one error finish for %s", got, runID)
	}
	r.Cancel(runID)
	r.Cancel("run-unknown")
}

func TestCancelStopsOnlyItsOwnRun(t *testing.T) {
	r, _ := newTestRunner(t)
	finishes, wait := watchFinishes(t)
	started := make(chan struct{}, 2)
	if err := r.RegisterObservable(func(*sql.DB) ObservableCommand { return waitForCancel{started} }); err != nil {
		t.Fatal(err)
	}

	first, _ := r.Begin("test.wait", nil)
	second, _ := r.Begin("test.wait", nil)
	<-started
	<-started
	r.Cancel(first)
	wait(1)
	if got := finishes(); len(got) != 1 || got[0].RunID != first {
		t.Fatalf("finishes = %+v, want only %s", got, first)
	}
	r.Cancel(second)
	wait(2)
}

func TestAPanickingCommandStillFinishes(t *testing.T) {
	r, _ := newTestRunner(t)
	finishes, wait := watchFinishes(t)
	if err := r.RegisterObservable(func(*sql.DB) ObservableCommand { return panics{} }); err != nil {
		t.Fatal(err)
	}

	runID, _ := r.Begin("test.panic", nil)
	wait(1)

	if got := finishes(); len(got) != 1 || got[0].RunID != runID || got[0].Error == "" {
		t.Errorf("finishes = %+v", got)
	}
}
