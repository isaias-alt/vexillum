// Package cmdname holds the name of the command a user types to run
// vexillum. The product is still called "vexillum" (docs, site, repo,
// the .vexillum/ state folders); only the executable is "vx". Every
// usage string, error message, help text and template that tells the
// user to run the tool builds it from Name, so there is a single place
// to change.
package cmdname

// Name is the one and only executable name vexillum installs.
const Name = "vx"
