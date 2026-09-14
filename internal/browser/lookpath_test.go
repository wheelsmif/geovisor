package browser

import (
	"os"
	"strings"
	"testing"
)

func TestLookPathFindsWellKnownWindowsChrome(t *testing.T) {
	t.Parallel()

	known := `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`
	if _, err := os.Stat(known); err != nil {
		t.Skip("well-known Windows Chrome path is not present")
	}
	found, ok := LookPath()
	if !ok {
		t.Fatal("LookPath missed Chrome at the well-known Windows install path")
	}
	if _, err := os.Stat(found); err != nil {
		t.Fatalf("LookPath returned %q: %v", found, err)
	}
}

func TestExtraChromiumPathsIncludesProgramFilesX86(t *testing.T) {
	t.Parallel()

	var found bool
	for _, candidate := range extraChromiumPaths() {
		normalized := strings.ToLower(candidate)
		if strings.Contains(normalized, `program files (x86)`) &&
			strings.Contains(normalized, `chrome.exe`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("extra Chromium paths omitted the 32-bit Program Files Chrome location")
	}
}
