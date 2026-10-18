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

func TestBrowserCandidatesKeepRodPlatformPaths(t *testing.T) {
	t.Parallel()

	darwin := browserCandidates("darwin")
	if !containsPath(darwin, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome") {
		t.Fatalf("darwin search = %v", darwin)
	}
	linux := browserCandidates("linux")
	if !containsPath(linux, "/usr/bin/google-chrome") || !containsPath(linux, "/snap/bin/chromium") {
		t.Fatalf("linux search = %v", linux)
	}
	windows := browserCandidates("windows")
	if !containsPath(windows, "chrome") {
		t.Fatalf("windows search = %v", windows)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
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
