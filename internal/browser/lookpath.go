package browser

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LookPath locates a Chromium-family executable. Windows also searches the
// 32-bit Program Files tree when ProgramFiles(x86) is absent from the process
// environment.
func LookPath() (string, bool) {
	for _, candidate := range browserCandidates(runtime.GOOS) {
		if found, ok := resolveExecutable(candidate); ok {
			return found, true
		}
	}
	for _, candidate := range extraChromiumPaths() {
		if found, ok := resolveExecutable(candidate); ok {
			return found, true
		}
	}
	return "", false
}

// browserCandidates is the per-OS executable search.
func browserCandidates(goos string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	case "linux":
		return []string{
			"chrome",
			"google-chrome",
			"/usr/bin/google-chrome",
			"microsoft-edge",
			"/usr/bin/microsoft-edge",
			"chromium",
			"chromium-browser",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
			"/data/data/com.termux/files/usr/bin/chromium-browser",
		}
	case "openbsd":
		return []string{"chrome", "chromium"}
	case "windows":
		return append([]string{"chrome", "edge"}, expandWindowsExePaths(
			`Google\Chrome\Application\chrome.exe`,
			`Chromium\Application\chrome.exe`,
			`Microsoft\Edge\Application\msedge.exe`,
		)...)
	default:
		return nil
	}
}

func expandWindowsExePaths(list ...string) []string {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LocalAppData"),
	}
	out := make([]string, 0, len(list)*len(roots))
	for _, relative := range list {
		for _, root := range roots {
			if root == "" {
				continue
			}
			out = append(out, filepath.Join(root, relative))
		}
	}
	return out
}

func resolveExecutable(candidate string) (string, bool) {
	if candidate == "" {
		return "", false
	}
	if strings.ContainsAny(candidate, `/\`) || filepath.IsAbs(candidate) {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
		return "", false
	}
	found, err := exec.LookPath(candidate)
	if err != nil {
		return "", false
	}
	return found, true
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
