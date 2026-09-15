package compiler

import (
	"regexp"
	"testing"
)

func TestLowValuePatternsEmbedded(t *testing.T) {
	t.Parallel()

	patterns, err := parseLowValuePatterns(lowValuePatternsJSON)
	if err != nil {
		t.Fatalf("embedded lowvalue_patterns.json: %v", err)
	}

	cases := []struct {
		name    string
		pattern string
		flags   string
		sample  string
	}{
		{name: "citationMarkName", pattern: patterns.CitationMarkName, sample: "[1]"},
		{name: "doiName", pattern: patterns.DOIName, flags: "(?i)", sample: "10.1000/xyz123"},
		{name: "bareRFCOrDOI", pattern: patterns.BareRFCOrDOI, flags: "(?i)", sample: "RFC 9110"},
		{
			name:    "citationFragment",
			pattern: patterns.CitationFragment,
			flags:   "(?i)",
			sample:  `a[href="#cite_note-HTML-1"]`,
		},
	}
	for _, tc := range cases {
		re, err := regexp.Compile(tc.flags + tc.pattern)
		if err != nil {
			t.Fatalf("%s: compile %q: %v", tc.name, tc.pattern, err)
		}
		if !re.MatchString(tc.sample) {
			t.Fatalf("%s did not match %q", tc.name, tc.sample)
		}
	}
}
