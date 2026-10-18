package browser

import (
	"strings"
	"testing"
)

func TestSanitizeTextReplacesLongestQueryValueFirst(t *testing.T) {
	t.Parallel()
	secret := "https://example.test/?token=secret&token=secret-value"
	got := sanitizeText("leaked secret-value", secret)
	if strings.Contains(got, "secret") || strings.Contains(got, "-value") {
		t.Fatalf("sanitized = %q, want the longer query value replaced before its prefix", got)
	}
}

func TestDecodeBatchStrictRejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"frames":[],"interactions":[],"warnings":[]}`)
	if _, err := decodeBatchStrict(payload); err != nil {
		t.Fatalf("small payload: %v", err)
	}
	oversized := make([]byte, MaxObservationPayloadBytes+1)
	copy(oversized, payload)
	if _, err := decodeBatchStrict(oversized); err == nil {
		t.Fatal("oversized payload was accepted")
	}
}
