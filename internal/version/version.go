package version

import "fmt"

// Version and Commit are injected at link time. Host `go build` without
// -ldflags keeps the development fallbacks.
var (
	Version = "dev"
	Commit  = "unknown"
)

func Line(component string) string {
	return fmt.Sprintf("%s %s (%s)", component, Version, Commit)
}
