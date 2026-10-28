package doctorcheck

import "sync"

// Check is one step of the doctor report: it produces zero or more results.
// A step that starts a process (git, a tool's --version, ps, the Stop hook
// probe) is a Check of its own so RunAll can overlap it with the others.
type Check func() []Result

// One makes a Check of a function that returns a single result.
func One(f func() Result) Check {
	return func() []Result { return []Result{f()} }
}

// Fixed makes a Check of results that are already known.
func Fixed(results ...Result) Check {
	return func() []Result { return results }
}

// RunAll runs the checks with at most limit of them at once (one at a time
// when limit is below 1) and returns their results concatenated in the order
// of checks, whatever order they finished in, so a report reads the same as if
// they had run one after another. Checks must not depend on each other and
// must be safe to run side by side. It returns when every check has.
func RunAll(limit int, checks []Check) []Result {
	limit = max(limit, 1)
	results := make([][]Result, len(checks))

	var wg sync.WaitGroup
	slots := make(chan struct{}, limit)
	for i, check := range checks {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			results[i] = check()
		}()
	}
	wg.Wait()

	var out []Result
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}
