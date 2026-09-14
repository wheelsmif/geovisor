// Package pageurl normalizes page URLs and origins from raw strings.
// It has no browser-library or emitter coupling.
package pageurl

import (
	"net/url"
	"strings"
)

// PageURL keeps scheme, host, and path and drops query and fragment so tokens
// in iframe src and source URLs cannot enter TIR (P12). Internal whitespace is
// collapsed the same way compiler text is cleaned.
func PageURL(raw string) string {
	trimmed := strings.Join(strings.Fields(raw), " ")
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	if parsed.Host == "" {
		return parsed.String()
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
}

// Origin returns scheme://host for an absolute URL, or empty when the value
// has no usable origin.
func Origin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
