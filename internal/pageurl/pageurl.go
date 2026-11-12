// Package pageurl normalizes page URLs and origins from raw strings.
// It has no browser-library or emitter coupling.
package pageurl

import (
	"net/url"
	"strings"
)

// maxUnparsedURL bounds a URL that could not be parsed, so a raw query or a
// page-sized blob cannot enter TIR identity.
const maxUnparsedURL = 256

// PageURL keeps scheme, host, and path and drops query and fragment so tokens
// in iframe src and source URLs cannot enter TIR. Non-hierarchical schemes
// such as data: and blob: keep only the scheme, because the opaque part can
// be a UUID or the page itself. about:blank and about:srcdoc stay stable
// tokens. Internal whitespace is collapsed the same way compiler text is cleaned.
func PageURL(raw string) string {
	trimmed := strings.Join(strings.Fields(raw), " ")
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return boundUnparsedURL(trimmed)
	}
	if parsed.Scheme == "about" && parsed.Opaque == "srcdoc" {
		return "about:srcdoc"
	}
	if parsed.Scheme == "about" && parsed.Opaque == "blank" {
		return "about:blank"
	}
	// data:, blob:, mailto:, and javascript: keep the scheme only. A second
	// pass sees that token with an empty opaque part; it must stay the token
	// so a data: frame and a blob: frame do not collapse onto an empty src.
	if opaqueScheme(parsed.Scheme) && parsed.Host == "" {
		return parsed.Scheme + ":"
	}
	if nonHierarchical(parsed) {
		return parsed.Scheme + ":"
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	if parsed.Host == "" {
		return boundUnparsedURL(parsed.EscapedPath())
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
}

// Origin returns scheme://host for an absolute URL, or empty when the value
// has no usable origin. about:srcdoc is the stable token for srcdoc documents,
// whose browser security origin is otherwise "://".
func Origin(raw string) string {
	if page := PageURL(raw); page == "about:srcdoc" {
		return "about:srcdoc"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// NormalizeSecurityOrigin turns a browser security origin into a TIR origin.
// Chromium reports "://" for about:srcdoc; that is replaced with about:srcdoc.
func NormalizeSecurityOrigin(securityOrigin, page string) string {
	origin := strings.TrimSpace(securityOrigin)
	if origin == "://" || origin == "" {
		if PageURL(page) == "about:srcdoc" || strings.Contains(page, "about:srcdoc") {
			return "about:srcdoc"
		}
		return ""
	}
	if normalized := Origin(origin); normalized != "" {
		return normalized
	}
	if origin == "about:srcdoc" {
		return "about:srcdoc"
	}
	return ""
}

func opaqueScheme(scheme string) bool {
	switch scheme {
	case "data", "blob", "mailto", "javascript":
		return true
	default:
		return false
	}
}

func nonHierarchical(parsed *url.URL) bool {
	return parsed.Scheme != "" && parsed.Opaque != "" && parsed.Host == ""
}

func boundUnparsedURL(raw string) string {
	cut := raw
	if index := strings.IndexAny(raw, "?#"); index >= 0 {
		cut = raw[:index]
	}
	runes := []rune(cut)
	if len(runes) > maxUnparsedURL {
		return string(runes[:maxUnparsedURL])
	}
	return cut
}
