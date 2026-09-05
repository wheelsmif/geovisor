// Package browser observes Chromium pages and returns browser-independent
// compiler input. It owns browser processes only in launch mode.
package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/wheelsmif/geovisor/internal/compiler"
	"github.com/wheelsmif/geovisor/internal/observation"
)

const (
	defaultTimeout       = 30 * time.Second
	defaultDOMQuiet      = 250 * time.Millisecond
	defaultDOMQuietLimit = 2 * time.Second
)

// BrowserSource is the small boundary shared by launch and attach sources.
type BrowserSource interface {
	Observe(context.Context) (Result, error)
}

// Result includes compiler input and non-fatal, typed diagnostics. A result
// may contain useful partial observations when diagnostics are present.
type Result struct {
	Input       compiler.Input
	Diagnostics []Diagnostic
}

// DiagnosticSeverity identifies the effect of a non-fatal browser condition.
type DiagnosticSeverity string

const (
	SeverityInfo    DiagnosticSeverity = "info"
	SeverityWarning DiagnosticSeverity = "warning"
)

// DiagnosticCode is stable for callers that need to branch on diagnostics.
type DiagnosticCode string

const (
	DiagnosticDOMNotQuiet        DiagnosticCode = "dom_not_quiet"
	DiagnosticFrameUncovered     DiagnosticCode = "frame_uncovered"
	DiagnosticAccessInterstitial DiagnosticCode = "access_interstitial"
)

// Diagnostic describes a condition that did not prevent returning a result.
type Diagnostic struct {
	Code      DiagnosticCode
	Severity  DiagnosticSeverity
	Message   string
	FramePath []observation.FrameReference
}

// ErrorCode is stable for callers that need to branch on fatal source errors.
type ErrorCode string

const (
	ErrorInvalidConfiguration ErrorCode = "invalid_configuration"
	ErrorLaunch               ErrorCode = "launch_failed"
	ErrorConnect              ErrorCode = "connect_failed"
	ErrorTargetNotFound       ErrorCode = "target_not_found"
	ErrorNavigation           ErrorCode = "navigation_failed"
	ErrorReadiness            ErrorCode = "readiness_failed"
	ErrorFrameDiscovery       ErrorCode = "frame_discovery_failed"
	ErrorCanceled             ErrorCode = "canceled"
	ErrorTimeout              ErrorCode = "timeout"
)

// Error is a sanitized failure at the browser package boundary.
type Error struct {
	Code    ErrorCode
	Stage   string
	Message string
	err     error
}

func (e *Error) Error() string {
	if e.Stage == "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("%s: %s (%s)", e.Stage, e.Message, e.Code)
}

// Unwrap returns only a sanitized underlying error.
func (e *Error) Unwrap() error {
	return e.err
}

// ExtractionOptions are passed to the committed browser payload.
type ExtractionOptions struct {
	SafeExplore   bool `json:"safeExplore"`
	MaxDepth      int  `json:"maxDepth,omitempty"`
	MaxOperations int  `json:"maxOperations,omitempty"`
	TimeoutMS     int  `json:"timeoutMs,omitempty"`
}

// LaunchOptions configure an isolated Chromium process. Headless defaults to
// true when nil. Stealth only removes common automation markers and provides
// no guarantee of bypassing bot detection.
type LaunchOptions struct {
	URL             string
	ExecutablePath  string
	Headless        *bool
	Timeout         time.Duration
	DOMQuietPeriod  time.Duration
	DOMQuietTimeout time.Duration
	Stealth         bool
	Extraction      ExtractionOptions
}

// AttachOptions configure observation of an existing Chromium CDP endpoint.
// RequestedURL is optional; when empty, attach mode never navigates.
type AttachOptions struct {
	Endpoint        string
	RequestedURL    string
	Selector        TargetSelector
	Timeout         time.Duration
	DOMQuietPeriod  time.Duration
	DOMQuietTimeout time.Duration
	Extraction      ExtractionOptions
}

// TargetSelectionMode determines how attach mode chooses a top-level page.
type TargetSelectionMode string

const (
	SelectActiveTopLevel TargetSelectionMode = "active_top_level"
	SelectExactTargetID  TargetSelectionMode = "exact_target_id"
	SelectExactURL       TargetSelectionMode = "exact_url"
)

// TargetSelector is explicit and deterministic. The zero value selects the
// focused top-level page, falling back to canonical URL/title order.
type TargetSelector struct {
	Mode     TargetSelectionMode
	TargetID string
	URL      string
}

type sourceMode uint8

const (
	modeLaunch sourceMode = iota + 1
	modeAttach
)

type source struct {
	mode   sourceMode
	launch LaunchOptions
	attach AttachOptions
}

// NewLaunch validates options and returns a launch-mode source.
func NewLaunch(options LaunchOptions) (BrowserSource, error) {
	if err := validateLaunchOptions(&options); err != nil {
		return nil, err
	}
	return &source{mode: modeLaunch, launch: options}, nil
}

// NewAttach validates options and returns an attach-mode source.
func NewAttach(options AttachOptions) (BrowserSource, error) {
	if err := validateAttachOptions(&options); err != nil {
		return nil, err
	}
	return &source{mode: modeAttach, attach: options}, nil
}
