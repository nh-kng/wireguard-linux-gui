package statuspoll

import (
	"errors"
	"testing"
	"time"
)

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for background query")
		var zero T
		return zero
	}
}

func TestRefreshCoalescesAndDiscardsStaleResults(t *testing.T) {
	started := make(chan struct{}, 2)
	results := make(chan int)
	ui := make(chan func(), 2)
	var applied []int
	p := New(func() (int, error) {
		started <- struct{}{}
		return <-results, nil
	}, func(f func()) { ui <- f }, func(value int, err error) {
		applied = append(applied, value)
	})
	defer p.Close()
	p.Refresh()
	receive(t, started)
	// Simulate several actions while an older system query is blocked.
	for n := 0; n < 10; n++ {
		p.Refresh()
	}
	select {
	case <-started:
		t.Fatal("queries overlapped")
	default:
	}
	results <- 1
	receive(t, ui)()
	if len(applied) != 0 {
		t.Fatal("displayed a snapshot from before the action")
	}
	receive(t, started)
	results <- 2
	receive(t, ui)()
	if len(applied) != 1 || applied[0] != 2 {
		t.Fatalf("applied %v; want only the fresh snapshot [2]", applied)
	}
	select {
	case <-started:
		t.Fatal("burst of refresh requests was not coalesced")
	default:
	}
}

func TestQueryErrorsReachUI(t *testing.T) {
	ui := make(chan func(), 1)
	want := errors.New("WireGuard unavailable")
	var got error
	p := New(func() (int, error) { return 0, want }, func(f func()) { ui <- f }, func(_ int, err error) {
		got = err
	})
	defer p.Close()
	p.Refresh()
	receive(t, ui)()
	if !errors.Is(got, want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}

func TestCloseDiscardsInflightResultAndPendingRefresh(t *testing.T) {
	ui := make(chan func(), 1)
	results := make(chan int)
	started := make(chan struct{}, 2)
	p := New(func() (int, error) {
		started <- struct{}{}
		return <-results, nil
	}, func(f func()) { ui <- f }, func(_ int, _ error) {
		t.Fatal("updated UI after shutdown")
	})
	p.Refresh()
	receive(t, started)
	p.Refresh()
	p.Close()
	results <- 1
	receive(t, ui)()
	p.Refresh()
	select {
	case <-started:
		t.Fatal("started a query after shutdown")
	default:
	}
}
