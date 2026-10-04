// Package schemaver decides whether a schema_version read from a state file
// is one this build can load.
//
// Public schema numbering restarted at Current (0) while vexillum is
// pre-release. State written before the reset carries the old numbers, so
// every file type declares the legacy numbers it once used: those are known
// older versions and still load. Anything else is a version this build does
// not know and is refused, which keeps the "newer vexillum wrote this" guard
// meaningful.
package schemaver

// Current is the schema version every file type writes today.
const Current = 0

// Supported reports whether got is the current version or one of the legacy
// numbers a file type used before the reset. A later bump of a file type
// must pick a number outside its legacy set, or an old file and a new one
// would be indistinguishable.
func Supported(got int, legacy ...int) bool {
	if got == Current {
		return true
	}
	for _, v := range legacy {
		if got == v {
			return true
		}
	}
	return false
}
