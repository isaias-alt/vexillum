// Package schemaver decides whether a schema_version read from a state file
// is one this build can load.
//
// A file type that has never changed shape writes Current and accepts only
// it (Supported). A file type that gained a field older builds cannot honor
// declares its own version and accepts the range of versions it can still
// read (SupportedRange). Either way, anything outside the accepted versions
// is one this build does not know and is refused, which keeps the "newer
// vexillum wrote this" guard meaningful.
package schemaver

// Current is the schema version of the file types that have not changed
// shape since the first release.
const Current = 0

// Supported reports whether got is the version this build writes.
func Supported(got int) bool {
	return got == Current
}

// SupportedRange reports whether got lies in [oldest, current]: the
// versions a file type that has been bumped compatibly can read. A file
// written at oldest decodes with the newer fields at their zero value.
func SupportedRange(got, oldest, current int) bool {
	return got >= oldest && got <= current
}
