// Package payload embeds the reproducibly built, browser-local extraction
// payload. Browser lifecycle and CDP invocation remain the caller's concern.
package payload

import _ "embed"

// browserBundle is generated from client/src by the pinned esbuild toolchain.
//
//go:embed extractor.js
var browserBundle string

// BrowserBundle returns the JavaScript payload that installs
// globalThis.__GEOVISOR_EXTRACT__ in the current browser execution context.
func BrowserBundle() string {
	return browserBundle
}
