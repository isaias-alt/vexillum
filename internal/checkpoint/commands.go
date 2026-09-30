package checkpoint

// runLint and runTests autodetect which command to run from the project
// markers already sitting in campPath - no per-project checkpoint config
// to set up first (same "no separate setup step" posture the general
// asked for when review-tool' own gating moved from "vexillum init" to
// lazy-on-first-ship; here there's simply nothing left to configure).
// Neither marker present means "vexillum ship" is being used against a
// project this pipeline doesn't yet know how to lint or test - Passed
// stays true rather than failing a mission for tooling gaps outside its
// control, but Detail says so plainly.

func runLint(campPath string) (StepResult, error) {
	switch {
	case hasFile(campPath, "go.mod"):
		return runCommand(StepLint, campPath, "go", "vet", "./...")
	case hasFile(campPath, "package.json"):
		return runCommand(StepLint, campPath, "npm", "run", "lint", "--if-present")
	default:
		return StepResult{Step: StepLint, Passed: true, Detail: "no lint command detected (no go.mod or package.json)"}, nil
	}
}

func runTests(campPath string) (StepResult, error) {
	switch {
	case hasFile(campPath, "go.mod"):
		return runCommand(StepTests, campPath, "go", "test", "./...")
	case hasFile(campPath, "package.json"):
		return runCommand(StepTests, campPath, "npm", "run", "test", "--if-present")
	default:
		return StepResult{Step: StepTests, Passed: true, Detail: "no test command detected (no go.mod or package.json)"}, nil
	}
}
