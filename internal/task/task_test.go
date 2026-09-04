package task

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rustbohr/repohop/internal/model"
)

func repos(n int) []model.Repo {
	out := make([]model.Repo, n)
	for i := range out {
		out[i] = model.Repo{Name: string(rune('a' + i)), Path: "/r/" + string(rune('a'+i))}
	}
	return out
}

func TestCollectPreservesInputOrder(t *testing.T) {
	in := repos(10)
	results := Collect(context.Background(), in, 4, func(_ context.Context, repo model.Repo) (string, error) {
		return repo.Name, nil
	})

	if len(results) != len(in) {
		t.Fatalf("got %d results, want %d", len(results), len(in))
	}
	for i, result := range results {
		if result.Index != i || result.Value != in[i].Name {
			t.Fatalf("result %d = %+v, want the value for %q", i, result, in[i].Name)
		}
	}
}

func TestCollectCarriesErrors(t *testing.T) {
	want := errors.New("boom")
	results := Collect(context.Background(), repos(3), 2, func(_ context.Context, repo model.Repo) (int, error) {
		if repo.Name == "b" {
			return 0, want
		}
		return 1, nil
	})
	if !errors.Is(results[1].Err, want) {
		t.Errorf("results[1].Err = %v, want %v", results[1].Err, want)
	}
	if results[0].Err != nil || results[2].Err != nil {
		t.Error("an error in one repository leaked into the others")
	}
}

func TestStreamRespectsConcurrencyLimit(t *testing.T) {
	const limit = 3
	var mu sync.Mutex
	var running, peak int

	results := Collect(context.Background(), repos(20), limit, func(_ context.Context, _ model.Repo) (int, error) {
		mu.Lock()
		running++
		if running > peak {
			peak = running
		}
		mu.Unlock()

		// Busy just long enough for overlap to be observable.
		for i := 0; i < 10000; i++ {
			_ = i
		}

		mu.Lock()
		running--
		mu.Unlock()
		return 0, nil
	})

	if len(results) != 20 {
		t.Fatalf("got %d results, want 20", len(results))
	}
	if peak > limit {
		t.Errorf("peak concurrency = %d, want at most %d", peak, limit)
	}
}

func TestStreamStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var started atomic.Int32

	out := Stream(ctx, repos(50), 1, func(ctx context.Context, _ model.Repo) (int, error) {
		started.Add(1)
		<-ctx.Done()
		return 0, ctx.Err()
	})
	cancel()

	for range out { //nolint:revive // drain
	}
	if got := started.Load(); got > 2 {
		t.Errorf("%d repositories started after cancellation, want the pool to stop promptly", got)
	}
}

func TestSequentialRunsInOrderAndReports(t *testing.T) {
	var order, reported []string
	results := Sequential(context.Background(), repos(4), func(_ context.Context, repo model.Repo) (string, error) {
		order = append(order, repo.Name)
		return repo.Name, nil
	}, func(result Result[string]) {
		reported = append(reported, result.Value)
	})

	want := []string{"a", "b", "c", "d"}
	for i := range want {
		if order[i] != want[i] || reported[i] != want[i] || results[i].Value != want[i] {
			t.Fatalf("step %d: ran %q, reported %q, result %q, want %q", i, order[i], reported[i], results[i].Value, want[i])
		}
	}
}

func TestEmptyInput(t *testing.T) {
	if got := Collect(context.Background(), nil, 4, func(context.Context, model.Repo) (int, error) {
		t.Fatal("the operation ran with no repositories")
		return 0, nil
	}); len(got) != 0 {
		t.Errorf("Collect(nil) = %v, want no results", got)
	}
}
