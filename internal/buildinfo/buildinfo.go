// Package buildinfo holds the version this binary was built as, so packages
// below cmd/vx (the sentinel records it, doctor compares it) can read it
// without importing package main.
package buildinfo

// Version is the release version, "dev" for a plain "go build". cmd/vx sets it
// at startup from its own linker-set variable.
var Version = "dev"
