package main

import "sync"

// runPool runs tasks[i] for every i in [0,n) using at most workers goroutines.
// It is a small bounded worker pool built on stdlib primitives (a semaphore
// channel plus a WaitGroup) so no external dependency is needed. Each task is
// expected to write its result into its own slot (indexed by i), so no shared
// state is mutated concurrently.
func runPool(n, workers int, task func(i int)) {
	if workers < 1 {
		workers = 1
	}
	if workers > n {
		workers = n
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		sem <- struct{}{} // acquire (blocks once `workers` are in flight)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }() // release
			task(i)
		}(i)
	}
	wg.Wait()
}
