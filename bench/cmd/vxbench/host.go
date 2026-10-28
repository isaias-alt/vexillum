package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// hostInfo identifies the machine a report was taken on. Numbers from two
// machines do not compare, so a baseline carries it and the comparison warns
// when it differs.
type hostInfo struct {
	// Label is a name for the machine; the host name unless -label says otherwise.
	Label     string `json:"label"`
	CPUModel  string `json:"cpu_model,omitempty"`
	CPUs      int    `json:"cpus"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
}

// vxInfo identifies the vx build that was timed.
type vxInfo struct {
	// Version is what `vx --version` printed.
	Version string `json:"version"`
	// Commit is the abbreviated commit the build came from, when known.
	Commit string `json:"commit,omitempty"`
	// Dirty says the working tree had uncommitted changes when it was built.
	Dirty bool `json:"dirty,omitempty"`
}

func detectHost(label string) hostInfo {
	if label == "" {
		label = shortHostname()
	}
	return hostInfo{
		Label:     label,
		CPUModel:  cpuModel(),
		CPUs:      runtime.NumCPU(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
	}
}

// shortHostname is the host name without its domain.
func shortHostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	short, _, _ := strings.Cut(name, ".")
	return short
}

// cpuModel names the processor, or "" when this platform has no cheap way to.
func cpuModel() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	case "linux":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err != nil {
			return ""
		}
		return parseCPUInfo(string(data))
	}
	return ""
}

var cpuInfoModel = regexp.MustCompile(`(?m)^model name\s*:\s*(.+)$`)

// parseCPUInfo reads the first "model name" line of /proc/cpuinfo.
func parseCPUInfo(data string) string {
	m := cpuInfoModel.FindStringSubmatch(data)
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(m[1]), " ")
}

// String is the one-line description used in report headers.
func (h hostInfo) String() string {
	parts := []string{h.Label}
	if h.CPUModel != "" {
		parts = append(parts, h.CPUModel)
	}
	parts = append(parts, fmt.Sprintf("%d CPUs", h.CPUs), h.OS+"/"+h.Arch, h.GoVersion)
	return strings.Join(parts, ", ")
}

// sameMachineAs reports whether two reports could have come from one machine:
// the label is a name, not a property, so it is left out.
func (h hostInfo) sameMachineAs(o hostInfo) bool {
	return h.OS == o.OS && h.Arch == o.Arch && h.CPUs == o.CPUs && h.CPUModel == o.CPUModel
}

// String is the identity of the build: its version, then its commit.
func (v vxInfo) String() string {
	switch {
	case v.Commit == "":
		return v.Version
	case v.Dirty:
		return fmt.Sprintf("%s (%s, dirty)", v.Version, v.Commit)
	}
	return fmt.Sprintf("%s (%s)", v.Version, v.Commit)
}

// loadAverage is the machine's one-minute load average, with ok false where
// the platform has no cheap way to read it. A busy machine makes every timing
// noisy, so a report records it.
func loadAverage() (load float64, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
		if err != nil {
			return 0, false
		}
		return parseLoadAverage(string(out))
	case "linux":
		data, err := os.ReadFile("/proc/loadavg")
		if err != nil {
			return 0, false
		}
		return parseLoadAverage(string(data))
	}
	return 0, false
}

// parseLoadAverage reads the first number of "{ 3.38 2.51 2.68 }" (macOS) or
// "3.38 2.51 2.68 1/512 12345" (Linux).
func parseLoadAverage(s string) (float64, bool) {
	fields := strings.Fields(strings.Trim(strings.TrimSpace(s), "{}"))
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	return v, err == nil
}

// busy reports whether a load average is high enough for the cores of the
// machine that timings taken under it should be read with care: more than
// half the cores kept occupied.
func (h hostInfo) busy(load float64) bool {
	return h.CPUs > 0 && load > float64(h.CPUs)/2
}
