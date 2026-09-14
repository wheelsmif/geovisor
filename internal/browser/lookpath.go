package browser

import (
	"os"
	"path/filepath"

	"github.com/go-rod/rod/lib/launcher"
)

// LookPath locates a Chromium-family executable. go-rod's search misses the
// 32-bit Program Files tree when ProgramFiles(x86) is absent from the process
// environment, which is how Windows Chrome installs were skipped locally.
func LookPath() (string, bool) {
	if found, ok := launcher.LookPath(); ok {
		return found, true
	}
	for _, candidate := range extraChromiumPaths() {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func extraChromiumPaths() []string {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LocalAppData"),
		`C:\Program Files (x86)`,
		`C:\Program Files`,
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		roots = append(roots, pf+" (x86)")
	}
	relatives := []string{
		filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join("Chromium", "Application", "chrome.exe"),
		filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roots)*len(relatives))
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, relative := range relatives {
			candidate := filepath.Join(root, relative)
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}
