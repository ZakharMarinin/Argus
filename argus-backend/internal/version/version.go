// Package version holds the collector's build version.
package version

// Version is set at build time:
//
//	go build -ldflags "-X argus-collector/internal/version.Version=v1.0.0" ./cmd/argus
//
// Builds without the flag report "dev".
var Version = "dev" //nolint:gochecknoglobals // must be a package var for -ldflags -X
