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
	applyTimingDefaults(&options.Timeout, &options.DOMQuietPeriod, &options.DOMQuietTimeout)
	if err := validateTimings(options.Timeout, options.DOMQuietPeriod, options.DOMQuietTimeout); err != nil {
		return configurationError("launch.timing", err)
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
	applyTimingDefaults(&options.Timeout, &options.DOMQuietPeriod, &options.DOMQuietTimeout)
	if err := validateTimings(options.Timeout, options.DOMQuietPeriod, options.DOMQuietTimeout); err != nil {
		return configurationError("attach.timing", err)
	}
	return nil
}

func applyTimingDefaults(timeout, quietPeriod, quietTimeout *time.Duration) {
	if *timeout == 0 {
		*timeout = defaultTimeout
	}
	if *quietPeriod == 0 {
		*quietPeriod = defaultDOMQuiet
	}
	if *quietTimeout == 0 {
		*quietTimeout = defaultDOMQuietLimit
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
				value = strings.ReplaceAll(value, parsed.User.String(), "[redacted]")
			}
			for key, values := range parsed.Query() {
				for _, item := range values {
					if item != "" {
						value = strings.ReplaceAll(value, item, "[redacted]")
					}
				}
				if key != "" {
					value = strings.ReplaceAll(value, key, "[redacted]")
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
	return parsed.String()
}
