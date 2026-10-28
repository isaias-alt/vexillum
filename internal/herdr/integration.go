package herdr

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// IntegrationState is what `herdr integration status` says about one agent's
// integration: the hook or extension herdr installs into that agent's own
// config so it reports its state (working, idle, blocked) instead of herdr
// having to read it off the screen.
type IntegrationState int

const (
	// IntegrationInstalled means the integration is present and current.
	IntegrationInstalled IntegrationState = iota
	// IntegrationNotInstalled means herdr has not installed it for the agent.
	IntegrationNotInstalled
	// IntegrationOutdated means it is installed but older than this herdr
	// ships.
	IntegrationOutdated
)

// IntegrationStatus runs `herdr integration status` and returns the state of
// the integration for the agent called name ("pi", "claude"), plus the
// detail herdr printed after the name ("outdated (v8 < v11)").
func IntegrationStatus(name string) (IntegrationState, string, error) {
	out, err := integrationStatusOutput()
	if err != nil {
		return 0, "", err
	}
	return parseIntegrationStatus(out, name)
}

func integrationStatusOutput() (string, error) {
	out, err := exec.Command("herdr", "integration", "status").Output()
	if err != nil {
		return "", fmt.Errorf("running herdr integration status: %w", err)
	}
	return string(out), nil
}

// IntegrationReader answers IntegrationStatus for any number of agents from a
// single `herdr integration status` run: herdr prints one line per agent, so
// asking once per agent only repeats the process. The first call runs it and
// the others, concurrent or later, wait for that run and share its output or
// its error. Meant to live for one report ("vx doctor"); a new reader runs it
// again. The zero value is ready to use.
type IntegrationReader struct {
	once sync.Once
	out  string
	err  error
}

// Status is IntegrationStatus backed by the reader's one run.
func (r *IntegrationReader) Status(name string) (IntegrationState, string, error) {
	r.once.Do(func() { r.out, r.err = integrationStatusOutput() })
	if r.err != nil {
		return 0, "", r.err
	}
	return parseIntegrationStatus(r.out, name)
}

// parseIntegrationStatus finds name's line in `herdr integration status`
// output, one per agent in the form "<name>: <state> (<path>)" ("pi: not
// installed (/home/me/.pi/agent/extensions/herdr-agent-state.ts)"), and reads
// the state off it. Any state text that is neither "not installed" nor
// "outdated" counts as installed. A name with no line is an error, so an
// output format this does not know is not mistaken for either answer.
func parseIntegrationStatus(out, name string) (IntegrationState, string, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		agent, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		if fields := strings.Fields(agent); len(fields) == 0 || fields[0] != name {
			continue
		}
		detail := strings.TrimSpace(rest)
		switch lower := strings.ToLower(detail); {
		case strings.HasPrefix(lower, "not installed"):
			return IntegrationNotInstalled, detail, nil
		case strings.HasPrefix(lower, "outdated"):
			return IntegrationOutdated, detail, nil
		default:
			return IntegrationInstalled, detail, nil
		}
	}
	return 0, "", fmt.Errorf("herdr integration status has no line for %q", name)
}
