// Package version exposes build version information.
package version

// Version is the build version reported by the command shell. Release builds
// inject a normalized tag with -ldflags "-X .../internal/version.Version=vX.Y.Z".
var Version = "0.0.0-dev"
