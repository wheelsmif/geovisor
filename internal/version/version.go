// Package version exposes build version information.
package version

import "runtime/debug"

// Version is the build version reported by the command shell. Release builds
// inject a normalized tag with -ldflags "-X .../internal/version.Version=vX.Y.Z".
var Version = "0.0.0-dev"

// Current reports Version unless it is the development placeholder, in which
// case a module version from the build info is used. go install builds then
// print the module version instead of always printing 0.0.0-dev. (devel) and
// an empty module version stay on the placeholder.
func Current() string {
	info, ok := debug.ReadBuildInfo()
	moduleVersion := ""
	if ok && info != nil {
		moduleVersion = info.Main.Version
	}
	return resolveVersion(Version, moduleVersion)
}

func resolveVersion(injected, moduleVersion string) string {
	if injected != "" && injected != "0.0.0-dev" {
		return injected
	}
	if moduleVersion == "" || moduleVersion == "(devel)" {
		if injected == "" {
			return "0.0.0-dev"
		}
		return injected
	}
	return moduleVersion
}
