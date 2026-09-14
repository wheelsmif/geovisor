package pageurl

import "testing"

func TestPageURLStripsQueryAndFragment(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ raw, want string }{
		{raw: "https://example.test/path?token=CURRENT-URL-SECRET#CURRENT-FRAGMENT-SECRET", want: "https://example.test/path"},
		{raw: "http://example.test:8080/", want: "http://example.test:8080/"},
		{raw: "about:blank", want: "about:blank"},
		{raw: "", want: ""},
		{raw: "  https://example.test/path  ", want: "https://example.test/path"},
		{raw: "https://example.test/foo  bar", want: "https://example.test/foo%20bar"},
	} {
		if got := PageURL(test.raw); got != test.want {
			t.Errorf("PageURL(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestOriginRejectsUnusableURLs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct{ raw, want string }{
		{raw: "https://example.test/path?query#fragment", want: "https://example.test"},
		{raw: "http://example.test:8080/", want: "http://example.test:8080"},
		{raw: "about:blank", want: ""},
		{raw: "data:text/html,hi", want: ""},
		{raw: "", want: ""},
	} {
		if got := Origin(test.raw); got != test.want {
			t.Errorf("Origin(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}
