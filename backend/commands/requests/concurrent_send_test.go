package requests_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"snap-rq/backend/models"
)

func TestCancelWhileWaitingForTheServerSavesNothing(t *testing.T) {
	h := newHarness(t)
	id := h.create(models.HttpRequest{Method: "GET", URL: h.url("/slow?ms=5000")})
	h.mustSend(h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")}), sendOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := h.send(id, sendOptions{ctx: ctx})

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("cancel took %s", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error = %v, want cancelled", err)
	}
	var req models.HttpRequest
	h.run("request.get", map[string]any{"id": id}, &req)
	if len(h.history(id)) != 0 || req.StatusCode != 0 || req.LastUsedAt != 0 {
		t.Errorf("a cancelled send left a trace: history %d, request %+v", len(h.history(id)), req)
	}
}

func TestManySendsAtOnceAllLand(t *testing.T) {
	h := newHarness(t)
	const requestsCount, sendsEach = 4, 6
	ids := make([]int64, requestsCount)
	for i := range ids {
		ids[i] = h.create(models.HttpRequest{Method: "GET", URL: h.url("/x")})
	}

	var wg sync.WaitGroup
	errs := make(chan error, requestsCount*sendsEach)
	for _, id := range ids {
		var req models.HttpRequest
		h.run("request.get", map[string]any{"id": id}, &req)
		for range sendsEach {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if res, err := h.send(id, sendOptions{}); err != nil {
					errs <- err
				} else if res.StatusCode != 200 {
					errs <- &sendError{res}
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	for _, id := range ids {
		if n := len(h.history(id)); n != sendsEach {
			t.Errorf("request %d has %d responses, want %d", id, n, sendsEach)
		}
	}
	if n := len(h.requests()); n != requestsCount*sendsEach {
		t.Errorf("the server got %d requests, want %d", n, requestsCount*sendsEach)
	}
}

type sendError struct{ res models.HttpResponse }

func (e *sendError) Error() string { return "send ended with " + e.res.Body }
