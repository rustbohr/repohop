// Package task fans a per-repository operation out across a bounded worker
// pool and streams the results back as they land, so the UI can fill rows in
// before every repository has reported.
package task

import (
	"context"
	"runtime"
	"sync"

	"github.com/rustbohr/repohop/internal/model"
)

// Result is one repository's outcome. Index is the repository's position in
// the input slice, so a streaming consumer can place the result without
// searching.
type Result[T any] struct {
	Index int
	Repo  model.Repo
	Value T
	Err   error
}

// Func is a per-repository operation.
type Func[T any] func(context.Context, model.Repo) (T, error)

// Stream runs fn against every repository with at most concurrency workers and
// returns a channel closed when the last result has been sent. Results arrive
// in completion order, not input order.
func Stream[T any](ctx context.Context, repos []model.Repo, concurrency int, fn Func[T]) <-chan Result[T] {
	out := make(chan Result[T], len(repos))
	workers := clampConcurrency(concurrency, len(repos))

	go func() {
		defer close(out)
		if len(repos) == 0 {
			return
		}

		jobs := make(chan int)
		var wg sync.WaitGroup
		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for i := range jobs {
					value, err := fn(ctx, repos[i])
					select {
					case out <- Result[T]{Index: i, Repo: repos[i], Value: value, Err: err}:
					case <-ctx.Done():
						return
					}
				}
			}()
		}

	dispatch:
		for i := range repos {
			select {
			case jobs <- i:
			case <-ctx.Done():
				break dispatch
			}
		}
		close(jobs)
		wg.Wait()
	}()

	return out
}

// Collect runs fn against every repository and returns the results in input
// order, once they have all finished.
func Collect[T any](ctx context.Context, repos []model.Repo, concurrency int, fn Func[T]) []Result[T] {
	results := make([]Result[T], len(repos))
	for i := range results {
		results[i] = Result[T]{Index: i, Repo: repos[i], Err: ctx.Err()}
	}
	for result := range Stream(ctx, repos, concurrency, fn) {
		results[result.Index] = result
	}
	return results
}

// Sequential runs fn against each repository in order, one at a time, calling
// report after each. Write operations use it: concurrent checkout output is
// hard to reason about when something fails halfway.
func Sequential[T any](ctx context.Context, repos []model.Repo, fn Func[T], report func(Result[T])) []Result[T] {
	results := make([]Result[T], 0, len(repos))
	for i, repo := range repos {
		if err := ctx.Err(); err != nil {
			results = append(results, Result[T]{Index: i, Repo: repo, Err: err})
			continue
		}
		value, err := fn(ctx, repo)
		result := Result[T]{Index: i, Repo: repo, Value: value, Err: err}
		results = append(results, result)
		if report != nil {
			report(result)
		}
	}
	return results
}

// clampConcurrency keeps the worker count sane whatever the config says.
func clampConcurrency(concurrency, jobs int) int {
	if concurrency <= 0 {
		concurrency = runtime.NumCPU() * 2
	}
	if concurrency > jobs {
		concurrency = jobs
	}
	if concurrency < 1 {
		concurrency = 1
	}
	return concurrency
}
