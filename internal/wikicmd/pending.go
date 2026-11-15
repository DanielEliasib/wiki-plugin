package wikicmd

import (
	"fmt"
	"io"
	"sort"
	"time"
)

// retryDelays are the waits before the 1st, 2nd and 3rd retry of a path; a path that fails a 4th time is given up
// until it changes again (or `wiki reindex` catches it).
var retryDelays = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}

// pendingSet is the watch loop's queue of changed and deleted paths, with how often each has failed. It has no
// I/O and no clock, so the retry behaviour can be tested without fsnotify.
type pendingSet struct {
	changed, deleted map[string]bool
	attempts         map[string]int
	taken            map[string]bool // path -> deleted, for the batch last taken
}

func newPendingSet() *pendingSet {
	return &pendingSet{changed: map[string]bool{}, deleted: map[string]bool{}, attempts: map[string]int{}}
}

// Add queues a path after a filesystem event; a new event resets the path's failures.
func (p *pendingSet) Add(path string, deleted bool) {
	delete(p.attempts, path)
	p.queue(path, deleted)
}

func (p *pendingSet) queue(path string, deleted bool) {
	if deleted {
		delete(p.changed, path)
		p.deleted[path] = true
	} else {
		delete(p.deleted, path)
		p.changed[path] = true
	}
}

func (p *pendingSet) Empty() bool { return len(p.changed) == 0 && len(p.deleted) == 0 }

// Take empties the set and returns its paths, sorted.
func (p *pendingSet) Take() (changed, deleted []string) {
	p.taken = map[string]bool{}
	for path := range p.changed {
		changed = append(changed, path)
		p.taken[path] = false
	}
	for path := range p.deleted {
		deleted = append(deleted, path)
		p.taken[path] = true
	}
	sort.Strings(changed)
	sort.Strings(deleted)
	p.changed, p.deleted = map[string]bool{}, map[string]bool{}
	return changed, deleted
}

// Retry settles the batch last taken: paths not in failed succeeded; the others are queued again and the delay
// before the next attempt is returned (0 when nothing was queued). Paths that have failed too often are returned in
// gaveUp and dropped.
func (p *pendingSet) Retry(failed []string) (delay time.Duration, gaveUp []string) {
	failedSet := map[string]bool{}
	for _, path := range failed {
		failedSet[path] = true
	}
	most := 0
	for path, wasDeleted := range p.taken {
		if !failedSet[path] {
			delete(p.attempts, path)
			continue
		}
		p.attempts[path]++
		if p.attempts[path] > len(retryDelays) {
			delete(p.attempts, path)
			gaveUp = append(gaveUp, path)
			continue
		}
		p.queue(path, wasDeleted)
		if p.attempts[path] > most {
			most = p.attempts[path]
		}
	}
	p.taken = nil
	sort.Strings(gaveUp)
	if most > 0 {
		delay = retryDelays[most-1]
	}
	return delay, gaveUp
}

// watchStep applies the pending batch and logs what went wrong. It returns how long to wait before the next
// attempt, or 0 when nothing is left to retry.
func watchStep(p *pendingSet, apply func(changed, deleted []string) (Outcome, error), log io.Writer, onBatch func(upserted, removed []string)) time.Duration {
	changed, deleted := p.Take()
	if len(changed) == 0 && len(deleted) == 0 {
		return 0
	}
	out, err := apply(changed, deleted)
	var failed []string
	if err != nil {
		fmt.Fprintf(log, "wiki watch: batch failed: %v\n", err)
		failed = append(append(failed, changed...), deleted...)
	}
	for _, f := range out.Failed {
		fmt.Fprintf(log, "wiki watch: %s: %v\n", f.Path, f.Err)
		failed = append(failed, f.Path)
	}
	if out.VaultIndexErr != nil {
		fmt.Fprintf(log, "wiki watch: could not write a vault index: %v\n", out.VaultIndexErr)
	}
	if onBatch != nil && (len(out.Upserted) > 0 || len(out.Removed) > 0) {
		onBatch(out.Upserted, out.Removed)
	}
	delay, gaveUp := p.Retry(failed)
	for _, path := range gaveUp {
		fmt.Fprintf(log, "wiki watch: giving up on %s until it changes; run `wiki reindex` to catch up\n", path)
	}
	if delay > 0 {
		fmt.Fprintf(log, "wiki watch: trying again in %s\n", delay)
	}
	return delay
}
