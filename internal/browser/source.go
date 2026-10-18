package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/wheelsmif/geovisor/internal/observation"
)

func (source *source) Observe(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, configurationError("observe.context", errors.New("context must not be nil"))
	}
	if source.mode == modeLaunch {
		return source.observeLaunch(ctx)
	}
	return source.observeAttach(ctx)
}

func (source *source) observeLaunch(parent context.Context) (result Result, err error) {
	ctx, cancel := context.WithTimeout(parent, source.launch.Timeout)
	defer cancel()

	executable := source.launch.ExecutablePath
	if executable == "" {
		var found bool
		executable, found = LookPath()
		if !found {
			return Result{}, sanitizedError(
				ErrorLaunch, "launch", "Chromium executable not found; set the browser executable path (--browser-executable)", nil,
			)
		}
	}
	if info, err := os.Stat(executable); err != nil || info.IsDir() {
		return Result{}, sanitizedError(
			ErrorLaunch, "launch.executable", "Chromium executable is not a file", err, executable,
		)
	}

	profile, err := os.MkdirTemp("", "geovisor-browser-*")
	if err != nil {
		return Result{}, sanitizedError(ErrorLaunch, "launch.profile", "create isolated profile", err)
	}
	defer func() {
		if removeErr := removeBrowserProfile(profile); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove browser profile: %w", removeErr))
		}
	}()

	headless := true
	if source.launch.Headless != nil {
		headless = *source.launch.Headless
	}
	process := newOwnedLauncher(ctx, executable, profile, headless, source.launch.hostResolverRules)
	if source.launch.Stealth {
		process.Delete("enable-automation").
			Set("disable-blink-features", "AutomationControlled")
	}

	controlURL, err := process.Launch()
	if err != nil {
		return Result{}, operationError(ctx, ErrorLaunch, "launch", "start Chromium", err, executable)
	}
	var connection *browserConnection
	defer func() {
		cleanupOwnedBrowser(connection, process)
	}()

	connection, err = connectBrowser(ctx, controlURL)
	if err != nil {
		return Result{}, operationError(ctx, ErrorConnect, "launch.connect", "connect to launched Chromium", err, controlURL)
	}
	created, err := (proto.TargetCreateTarget{URL: "about:blank"}).Call(connection.browser.Context(ctx))
	if err != nil {
		return Result{}, operationError(ctx, ErrorLaunch, "launch.target", "create browser target", err)
	}
	session, err := attachTarget(ctx, connection.browser, created.TargetID)
	if err != nil {
		return Result{}, operationError(ctx, ErrorConnect, "launch.attach", "attach to launched target", err)
	}
	defer detachTarget(connection.browser, session.sessionID)

	result, err = observeTarget(ctx, connection.browser, session, created.TargetID, observeTargetOptions{
		requestedURL:   source.launch.URL,
		navigate:       true,
		quietPeriod:    source.launch.DOMQuietPeriod,
		quietTimeout:   source.launch.DOMQuietTimeout,
		frameTimeout:   source.launch.FrameTimeout,
		extraction:     source.launch.Extraction,
		sourceKind:     observation.SourceLaunchURL,
		stealthEnabled: source.launch.Stealth,
	})
	if err != nil {
		return result, err
	}
	return result, nil
}

func (source *source) observeAttach(parent context.Context) (Result, error) {
	ctx, cancel := context.WithTimeout(parent, source.attach.Timeout)
	defer cancel()

	controlURL, err := resolveControlURL(ctx, source.attach.Endpoint)
	if err != nil {
		return Result{}, operationError(
			ctx, ErrorConnect, "attach.endpoint", "resolve Chromium CDP endpoint", err,
			source.attach.Endpoint,
		)
	}
	connection, err := connectBrowser(ctx, controlURL)
	if err != nil {
		return Result{}, operationError(
			ctx, ErrorConnect, "attach.connect", "connect to Chromium CDP endpoint", err,
			source.attach.Endpoint, controlURL,
		)
	}
	defer connection.closeTransport()

	target, err := chooseTarget(ctx, connection.browser, source.attach.Selector)
	if err != nil {
		return Result{}, operationError(
			ctx, ErrorTargetNotFound, "attach.select", "select browser target", err,
			source.attach.Endpoint,
		)
	}
	session, err := attachTarget(ctx, connection.browser, target.TargetID)
	if err != nil {
		return Result{}, operationError(ctx, ErrorConnect, "attach.target", "attach to browser target", err)
	}
	defer detachTarget(connection.browser, session.sessionID)

	return observeTarget(ctx, connection.browser, session, target.TargetID, observeTargetOptions{
		requestedURL: source.attach.RequestedURL,
		navigate:     source.attach.RequestedURL != "",
		quietPeriod:  source.attach.DOMQuietPeriod,
		quietTimeout: source.attach.DOMQuietTimeout,
		frameTimeout: source.attach.FrameTimeout,
		extraction:   source.attach.Extraction,
		sourceKind:   observation.SourceCDPAttach,
	})
}

func cleanupOwnedBrowser(connection *browserConnection, process *ownedBrowser) {
	if process == nil {
		return
	}
	if connection != nil {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultBrowserControlTimeout)
		_ = (proto.BrowserClose{}).Call(connection.browser.Context(ctx))
		cancel()
		connection.closeTransport()
	}
	// Browser.close is graceful but does not guarantee process exit on every
	// Chromium build. Kill is bounded and safe here because launch mode owns
	// the entire process tree.
	process.Kill()
}

// removeBrowserProfile deletes the isolated user-data directory. Chrome can
// still hold files briefly after the process tree is killed, so a short retry
// absorbs that race. A failure that survives the retry is returned and joined
// onto the observe error.
func removeBrowserProfile(profile string) error {
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		err = os.RemoveAll(profile)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}

func operationError(
	ctx context.Context,
	code ErrorCode,
	stage, message string,
	cause error,
	secrets ...string,
) *Error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return sanitizedError(ErrorTimeout, stage, "browser operation timed out", ctx.Err(), secrets...)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return sanitizedError(ErrorCanceled, stage, "browser operation canceled", ctx.Err(), secrets...)
	}
	return sanitizedError(code, stage, message, cause, secrets...)
}
