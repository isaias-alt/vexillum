// Package cmdname holds the name of the command a user types to run
// vexillum. The product is still called "vexillum" (docs, site, repo,
// the .vexillum/ state folders); only the executable is "vx". Every
// usage string, error message, help text and template that tells the
// user to run the tool builds it from Name, so there is a single place
// to change.
package cmdname

// Name is the executable name vexillum releases install.
const Name = "vx"

// DevName is the name scripts/install-local.sh gives the maintainer's own
// build, so it never takes Name from the Homebrew install.
const DevName = Name + "-dev"
