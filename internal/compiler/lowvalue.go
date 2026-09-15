package compiler

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
)

// Shared with client/src/shared/low-value.ts. Pattern strings are stored
// without inline (?i) or JavaScript flags; each language applies
// case-insensitivity where the historical predicates did.
//
//go:embed lowvalue_patterns.json
var lowValuePatternsJSON []byte

type lowValuePatterns struct {
	CitationMarkName string `json:"citationMarkName"`
	DOIName          string `json:"doiName"`
	BareRFCOrDOI     string `json:"bareRFCOrDOI"`
	CitationFragment string `json:"citationFragment"`
}

var (
	citationMarkName *regexp.Regexp
	doiName          *regexp.Regexp
	bareRFCOrDOI     *regexp.Regexp
	citationFragment *regexp.Regexp
)

func init() {
	patterns, err := parseLowValuePatterns(lowValuePatternsJSON)
	if err != nil {
		panic(err)
	}
	citationMarkName = mustCompileLowValue("citationMarkName", patterns.CitationMarkName)
	doiName = mustCompileLowValue("doiName", "(?i)"+patterns.DOIName)
	bareRFCOrDOI = mustCompileLowValue("bareRFCOrDOI", "(?i)"+patterns.BareRFCOrDOI)
	citationFragment = mustCompileLowValue("citationFragment", "(?i)"+patterns.CitationFragment)
}

func parseLowValuePatterns(raw []byte) (lowValuePatterns, error) {
	var patterns lowValuePatterns
	if err := json.Unmarshal(raw, &patterns); err != nil {
		return lowValuePatterns{}, fmt.Errorf("low-value patterns: %w", err)
	}
	switch {
	case patterns.CitationMarkName == "":
		return lowValuePatterns{}, fmt.Errorf("low-value patterns: missing citationMarkName")
	case patterns.DOIName == "":
		return lowValuePatterns{}, fmt.Errorf("low-value patterns: missing doiName")
	case patterns.BareRFCOrDOI == "":
		return lowValuePatterns{}, fmt.Errorf("low-value patterns: missing bareRFCOrDOI")
	case patterns.CitationFragment == "":
		return lowValuePatterns{}, fmt.Errorf("low-value patterns: missing citationFragment")
	}
	return patterns, nil
}

func isLowValueName(name, role string) bool {
	if role == "link" && name == "" {
		return true
	}
	return citationMarkName.MatchString(name) || doiName.MatchString(name) || bareRFCOrDOI.MatchString(name)
}

func mustCompileLowValue(name, pattern string) *regexp.Regexp {
	re, err := regexp.Compile(pattern)
	if err != nil {
		panic(fmt.Sprintf("low-value pattern %s: %v", name, err))
	}
	return re
}
