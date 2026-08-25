// Package buildinfo holds version metadata injected at build time via -ldflags.
package buildinfo

import "runtime"

// Version, Commit, and Date are overridden at build time with -ldflags -X.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// GoVersion returns the Go toolchain version used to build the binary.
func GoVersion() string {
	return runtime.Version()
}

// OS returns the target operating system the binary was built for.
func OS() string {
	return runtime.GOOS
}

// Arch returns the target architecture the binary was built for.
func Arch() string {
	return runtime.GOARCH
}
