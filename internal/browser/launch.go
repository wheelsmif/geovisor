package browser

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ownedBrowser starts one Chromium process with os/exec. It never imports
// rod/lib/launcher, so leakless is not linked into the binary.
type ownedBrowser struct {
	ctx        context.Context
	executable string
	profile    string
	flags      map[string][]string
	cmd        *exec.Cmd
	waited     chan struct{}
}

func newOwnedLauncher(
	ctx context.Context,
	executable, profile string,
	headless bool,
	resolverRules string,
) *ownedBrowser {
	flags := map[string][]string{
		"remote-debugging-port": {"0"},
		"user-data-dir":         {profile},
		"site-per-process":      nil,
		"disable-features":      {"TranslateUI"},
	}
	if headless {
		flags["headless"] = nil
	}
	if resolverRules != "" {
		flags["host-resolver-rules"] = []string{resolverRules}
	}
	return &ownedBrowser{
		ctx:        ctx,
		executable: executable,
		profile:    profile,
		flags:      flags,
	}
}

func (process *ownedBrowser) Set(name string, values ...string) *ownedBrowser {
	if len(values) == 0 {
		process.flags[name] = nil
		return process
	}
	process.flags[name] = append([]string(nil), values...)
	return process
}

func (process *ownedBrowser) Delete(name string) *ownedBrowser {
	delete(process.flags, name)
	return process
}

func (process *ownedBrowser) Has(name string) bool {
	_, ok := process.flags[name]
	return ok
}

func (process *ownedBrowser) GetFlags(name string) ([]string, bool) {
	values, ok := process.flags[name]
	if !ok {
		return nil, false
	}
	return append([]string(nil), values...), true
}

func (process *ownedBrowser) args() []string {
	names := make([]string, 0, len(process.flags))
	for name := range process.flags {
		names = append(names, name)
	}
	sort.Strings(names)
	args := make([]string, 0, len(names))
	for _, name := range names {
		values := process.flags[name]
		if len(values) == 0 {
			args = append(args, "--"+name)
			continue
		}
		args = append(args, "--"+name+"="+strings.Join(values, ","))
	}
	return args
}

// command is the process that Launch would start. Tests use it to prove the
// binary is Chromium itself and that no leakless helper is on the command line.
func (process *ownedBrowser) command() *exec.Cmd {
	return exec.Command(process.executable, process.args()...)
}

func (process *ownedBrowser) Launch() (string, error) {
	if process.cmd != nil {
		return "", errors.New("browser already launched")
	}
	absProfile, err := filepath.Abs(process.profile)
	if err != nil {
		return "", err
	}
	process.profile = absProfile
	process.flags["user-data-dir"] = []string{absProfile}

	cmd := exec.Command(process.executable, process.args()...)
	configureCommand(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", err
	}
	process.cmd = cmd
	process.waited = make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(process.waited)
	}()

	controlURL, err := readDevToolsActivePort(process.ctx, absProfile, process.waited)
	if err != nil {
		process.Kill()
		return "", err
	}
	return controlURL, nil
}

func (process *ownedBrowser) Kill() {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return
	}
	_ = killProcessTree(process.cmd.Process.Pid)
	if process.waited == nil {
		return
	}
	select {
	case <-process.waited:
	case <-time.After(DefaultBrowserControlTimeout):
	}
}

func readDevToolsActivePort(ctx context.Context, profile string, exited <-chan struct{}) (string, error) {
	path := filepath.Join(profile, "DevToolsActivePort")
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return parseDevToolsActivePort(data)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-exited:
			return "", errors.New("browser exited before DevToolsActivePort was written")
		case <-ticker.C:
		}
	}
}

func parseDevToolsActivePort(data []byte) (string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "", errors.New("DevToolsActivePort is empty")
	}
	port := strings.TrimSpace(lines[0])
	if _, err := strconv.Atoi(port); err != nil {
		return "", errors.New("DevToolsActivePort has no debugging port")
	}
	wsPath := "/devtools/browser"
	if len(lines) > 1 {
		candidate := strings.TrimSpace(lines[1])
		if candidate != "" {
			if !strings.HasPrefix(candidate, "/") {
				candidate = "/" + candidate
			}
			wsPath = candidate
		}
	}
	return "ws://127.0.0.1:" + port + wsPath, nil
}
