package browser

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func validateLaunchOptions(options *LaunchOptions) error {
	if _, err := validateTargetURL(options.URL); err != nil {
		return configurationError("launch.url", err)
	}
	if err := applyAndValidateTimings(
		&options.Timeout, &options.DOMQuietPeriod, &options.DOMQuietTimeout,
		&options.FrameTimeout, options.Extraction.TimeoutMS,
	); err != nil {
		return wrapTimingConfiguration("launch", err)
	}
	return nil
}

func validateAttachOptions(options *AttachOptions) error {
	if _, err := validateEndpoint(options.Endpoint); err != nil {
		return configurationError("attach.endpoint", err)
	}
	if options.RequestedURL != "" {
		if _, err := validateTargetURL(options.RequestedURL); err != nil {
			return configurationError("attach.requested_url", err)
		}
	}
	if err := validateSelector(&options.Selector); err != nil {
		return configurationError("attach.selector", err)
	}
	if err := applyAndValidateTimings(
		&options.Timeout, &options.DOMQuietPeriod, &options.DOMQuietTimeout,
		&options.FrameTimeout, options.Extraction.TimeoutMS,
	); err != nil {
		return wrapTimingConfiguration("attach", err)
	}
	return nil
}

func applyAndValidateTimings(
	timeout, quietPeriod, quietTimeout, frameTimeout *time.Duration,
	explorationMS int,
) error {
	applyTimingDefaults(timeout, quietPeriod, quietTimeout)
	applyFrameTimeoutDefault(frameTimeout, explorationMS)
	if err := validateTimings(*timeout, *quietPeriod, *quietTimeout); err != nil {
		return err
	}
	return validateFrameTimeout(*frameTimeout, explorationMS)
}

func wrapTimingConfiguration(prefix string, err error) error {
	if strings.HasPrefix(err.Error(), "frame timeout") {
		return configurationError(prefix+".frame_timeout", err)
	}
	return configurationError(prefix+".timing", err)
}

func applyTimingDefaults(timeout, quietPeriod, quietTimeout *time.Duration) {
	if *timeout == 0 {
		*timeout = DefaultTimeout
	}
	if *quietPeriod == 0 {
		*quietPeriod = DefaultDOMQuiet
	}
	if *quietTimeout == 0 {
		*quietTimeout = DefaultDOMQuietLimit
	}
}

func validateTimings(timeout, quietPeriod, quietTimeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if quietPeriod <= 0 {
		return errors.New("DOM quiet period must be positive")
	}
	if quietTimeout < quietPeriod {
		return errors.New("DOM quiet timeout must be at least the quiet period")
	}
	return nil
}

func applyFrameTimeoutDefault(frameTimeout *time.Duration, explorationMS int) {
	if *frameTimeout == 0 {
		*frameTimeout = defaultFrameTimeout(explorationBudget(explorationMS))
	}
}

func defaultFrameTimeout(exploration time.Duration) time.Duration {
	if exploration <= 0 {
		exploration = DefaultExplorationBudget
	}
	return exploration + DefaultSelectorAllowance + DefaultFrameOverhead
}

func explorationBudget(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		return DefaultExplorationBudget
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func validateFrameTimeout(frameTimeout time.Duration, explorationMS int) error {
	if frameTimeout <= 0 {
		return errors.New("frame timeout must be positive")
	}
	if frameTimeout < explorationBudget(explorationMS) {
		return errors.New("frame timeout must be at least the exploration timeout")
	}
	return nil
}

func validateTargetURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("URL is malformed")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("URL scheme must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("URL host is required")
	}
	if parsed.User != nil {
		return nil, errors.New("URL credentials are not allowed")
	}
	return parsed, nil
}

func validateEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("endpoint is malformed")
	}
	switch parsed.Scheme {
	case "http", "https", "ws", "wss":
	default:
		return nil, errors.New("endpoint scheme must be http, https, ws, or wss")
	}
	if parsed.Host == "" {
		return nil, errors.New("endpoint host is required")
	}
	if parsed.User != nil {
		return nil, errors.New("endpoint credentials are not allowed")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("endpoint fragment is not allowed")
	}
	return parsed, nil
}

func validateSelector(selector *TargetSelector) error {
	if selector.Mode == "" {
		selector.Mode = SelectActiveTopLevel
	}
	switch selector.Mode {
	case SelectActiveTopLevel:
		if selector.TargetID != "" || selector.URL != "" {
			return errors.New("active selector does not accept target ID or URL")
		}
	case SelectExactTargetID:
		if strings.TrimSpace(selector.TargetID) == "" || selector.URL != "" {
			return errors.New("exact target selector requires only a target ID")
		}
		selector.TargetID = strings.TrimSpace(selector.TargetID)
	case SelectExactURL:
		if selector.TargetID != "" {
			return errors.New("exact URL selector does not accept a target ID")
		}
		parsed, err := validateTargetURL(selector.URL)
		if err != nil {
			return err
		}
		if parsed.Path == "" {
			parsed.Path = "/"
		}
		selector.URL = parsed.String()
	default:
		return fmt.Errorf("unsupported selector mode %q", selector.Mode)
	}
	return nil
}

func configurationError(stage string, err error) error {
	return &Error{
		Code: ErrorInvalidConfiguration, Stage: stage,
		Message: err.Error(), err: errors.New(err.Error()),
	}
}

func sanitizedError(code ErrorCode, stage, message string, cause error, secrets ...string) *Error {
	var sanitized error
	if cause != nil {
		sanitized = errors.New(sanitizeText(cause.Error(), secrets...))
	}
	return &Error{Code: code, Stage: stage, Message: sanitizeText(message, secrets...), err: sanitized}
}

func sanitizeText(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		value = strings.ReplaceAll(value, secret, sanitizeURL(secret))
		if parsed, err := url.Parse(secret); err == nil {
			if parsed.User != nil {
				if username := parsed.User.Username(); len(username) >= 3 {
					value = strings.ReplaceAll(value, username, "[redacted]")
				}
				if password, set := parsed.User.Password(); set && password != "" {
					value = strings.ReplaceAll(value, password, "[redacted]")
				}
				value = strings.ReplaceAll(value, parsed.User.String(), "[redacted]")
			}
			for _, values := range parsed.Query() {
				for _, item := range values {
					// Single-character and two-character values are not secrets
					// worth scanning for; replacing them shreds ordinary
					// diagnostics (GV-006).
					if len(item) >= 3 {
						value = strings.ReplaceAll(value, item, "[redacted]")
					}
				}
			}
		}
	}
	return value
}

func sanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[redacted endpoint]"
	}
	if parsed.User != nil {
		parsed.User = url.User("[redacted]")
	}
	if parsed.RawQuery != "" {
		parsed.RawQuery = "redacted"
	}
	parsed.Fragment = ""
	parsed.Path = redactDevToolsPath(parsed.Path)
	parsed.RawPath = ""
	return parsed.String()
}

func redactDevToolsPath(path string) string {
	for _, prefix := range []string{"/devtools/browser/", "/devtools/page/"} {
		if strings.HasPrefix(path, prefix) && len(path) > len(prefix) {
			return prefix + "[redacted]"
		}
	}
	return path
}
