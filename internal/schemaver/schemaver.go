// Package schemaver decides whether a schema_version read from a state file
// is one this build can load.
//
// Every state file type writes Current. Anything else is a version this
// build does not know and is refused, which keeps the "newer vexillum wrote
// this" guard meaningful.
package schemaver

// Current is the schema version every file type writes today.
const Current = 0

// Supported reports whether got is the version this build writes.
func Supported(got int) bool {
	return got == Current
}
