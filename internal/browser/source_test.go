package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

func TestOwnedLauncherDisablesLeaklessAndPreservesIsolation(t *testing.T) {
	t.Parallel()
	instance := newOwnedLauncher(context.Background(), "chromium", t.TempDir(), true)
	if instance.Has(flags.Leakless) {
		t.Fatal("owned launcher must explicitly disable leakless")
	}
	if !instance.Has("site-per-process") {
		t.Fatal("owned launcher must preserve site-per-process isolation")
	}
	disabled, ok := instance.GetFlags("disable-features")
	if !ok || len(disabled) != 1 || disabled[0] != "TranslateUI" {
		t.Fatalf("disabled features = %v, want only TranslateUI", disabled)
	}
	if instance.Has("disable-site-isolation-trials") {
		t.Fatal("owned launcher must not disable site isolation trials")
	}
}

func TestTargetURLValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "http", raw: "http://example.test/path"},
		{name: "https", raw: "https://example.test/"},
		{name: "javascript", raw: "javascript:alert(1)", wantErr: true},
		{name: "file", raw: "file:///tmp/page.html", wantErr: true},
		{name: "missing host", raw: "https:///path", wantErr: true},
		{name: "credentials", raw: "https://user:pass@example.test/", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateTargetURL(test.raw)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateTargetURL(%q) error = %v, wantErr %v", test.raw, err, test.wantErr)
			}
		})
	}
}

func TestEndpointAndSelectorValidation(t *testing.T) {
	t.Parallel()
	options := AttachOptions{
		Endpoint: "http://127.0.0.1:9222",
		Selector: TargetSelector{Mode: SelectExactURL, URL: "https://example.test/path"},
	}
	if err := validateAttachOptions(&options); err != nil {
		t.Fatalf("validate attach options: %v", err)
	}
	if options.Timeout != DefaultTimeout || options.DOMQuietPeriod != DefaultDOMQuiet {
		t.Fatalf("timing defaults not applied: %+v", options)
	}

	invalid := options
	invalid.Endpoint = "file:///tmp/devtools"
	if err := validateAttachOptions(&invalid); err == nil {
		t.Fatal("file CDP endpoint unexpectedly accepted")
	}

	invalid = options
	invalid.Endpoint = "http://user:pass@127.0.0.1:9222"
	if err := validateAttachOptions(&invalid); err == nil {
		t.Fatal("CDP endpoint credentials unexpectedly accepted")
	}

	invalid = options
	invalid.Selector.TargetID = "also-set"
	if err := validateAttachOptions(&invalid); err == nil {
		t.Fatal("ambiguous target selector unexpectedly accepted")
	}
}

func TestSelectTargetDeterministically(t *testing.T) {
	t.Parallel()
	targets := []*proto.TargetTargetInfo{
		{TargetID: "z", Type: proto.TargetTargetInfoTypePage, URL: "https://z.example/", Title: "Z"},
		{TargetID: "b", Type: proto.TargetTargetInfoTypePage, URL: "https://a.example/", Title: "B"},
		{TargetID: "a", Type: proto.TargetTargetInfoTypePage, URL: "https://a.example/", Title: "A"},
		{TargetID: "internal", Type: proto.TargetTargetInfoTypePage, URL: "chrome://settings"},
		{TargetID: "worker", Type: proto.TargetTargetInfoTypeServiceWorker, URL: "https://a.example/sw.js"},
	}

	if _, err := selectTarget(targets, TargetSelector{Mode: SelectActiveTopLevel}); err == nil {
		t.Fatal("active selector unexpectedly chose a page among several top-level candidates")
	}

	_, err := selectTarget(
		[]*proto.TargetTargetInfo{
			{TargetID: "internal", Type: proto.TargetTargetInfoTypePage, URL: "chrome://settings"},
			{TargetID: "blank", Type: proto.TargetTargetInfoTypePage, URL: "about:blank"},
		},
		TargetSelector{Mode: SelectActiveTopLevel},
	)
	if err == nil || !strings.Contains(err.Error(), "found 0 HTTP(S) pages") ||
		!strings.Contains(err.Error(), "2 non-HTTP(S) tab(s) were ignored") {
		t.Fatalf("active selector error = %v", err)
	}

	selected, err := selectTarget(
		[]*proto.TargetTargetInfo{
			{TargetID: "only", Type: proto.TargetTargetInfoTypePage, URL: "https://a.example/"},
		},
		TargetSelector{Mode: SelectActiveTopLevel},
	)
	if err != nil || selected.TargetID != "only" {
		t.Fatalf("single active target = %v, %v; want only", selected, err)
	}

	selected, err = selectTarget(
		targets,
		TargetSelector{Mode: SelectExactURL, URL: "https://a.example/"},
	)
	if err != nil || selected.TargetID != "a" {
		t.Fatalf("exact URL target = %v, %v; want canonical duplicate a", selected, err)
	}
}

func TestSanitizedErrorRedactsEndpointSecrets(t *testing.T) {
	t.Parallel()
	endpoint := "ws://will:password@example.test/devtools/browser/id?token=secret"
	err := sanitizedError(
		ErrorConnect,
		"attach.connect",
		"connect "+endpoint,
		errors.New("dial "+endpoint+" with token secret"),
		endpoint,
	)
	rendered := err.Error() + " " + err.Unwrap().Error()
	for _, forbidden := range []string{"will", "password", "secret"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("sanitized error leaked %q: %s", forbidden, rendered)
		}
	}
	if !strings.Contains(rendered, "token") {
		t.Fatalf("sanitized error redacted the query key: %s", rendered)
	}
	if !strings.Contains(rendered, "redacted") {
		t.Fatalf("sanitized error has no redaction marker: %s", rendered)
	}
	if strings.Contains(rendered, "/devtools/browser/id") {
		t.Fatalf("sanitized error leaked the DevTools path token: %s", rendered)
	}
}

func TestSanitizeTextDoesNotShredShortQueryValues(t *testing.T) {
	t.Parallel()
	endpoint := "http://localhost:9222/?a=1&port=22"
	message := "cannot attach to target on port 1 of 22 remaining sessions"
	if got := sanitizeText(message, endpoint); got != message {
		t.Fatalf("short query values shredded the message:\n got %q\nwant %q", got, message)
	}
}

func TestSanitizeTextIgnoresQueryKeys(t *testing.T) {
	t.Parallel()
	endpoint := "http://localhost:9222/?a=1&user=bob"
	message := "cannot attach to target: a page was already claimed by another agent"
	if got := sanitizeText(message, endpoint); got != message {
		t.Fatalf("sanitizeText altered an unrelated message:\n got %q\nwant %q", got, message)
	}
	leaky := "cannot attach as user bob"
	if got := sanitizeText(leaky, endpoint); strings.Contains(got, "bob") {
		t.Fatalf("sanitizeText leaked query value: %s", got)
	}
	if !strings.Contains(sanitizeText(leaky, endpoint), "user") {
		t.Fatal("sanitizeText redacted the query key")
	}
}

func TestFrameTimeoutDefaultsProportionately(t *testing.T) {
	t.Parallel()
	options := LaunchOptions{URL: "https://example.test/"}
	if err := validateLaunchOptions(&options); err != nil {
		t.Fatalf("validate launch options: %v", err)
	}
	want := DefaultExplorationBudget + DefaultSelectorAllowance + DefaultFrameOverhead
	if options.FrameTimeout != want {
		t.Fatalf("default FrameTimeout = %s, want %s", options.FrameTimeout, want)
	}

	options = LaunchOptions{
		URL:          "https://example.test/",
		Extraction:   ExtractionOptions{TimeoutMS: 4000},
		FrameTimeout: 0,
	}
	if err := validateLaunchOptions(&options); err != nil {
		t.Fatalf("validate launch options with exploration: %v", err)
	}
	want = 4*time.Second + DefaultSelectorAllowance + DefaultFrameOverhead
	if options.FrameTimeout != want {
		t.Fatalf("proportionate FrameTimeout = %s, want %s", options.FrameTimeout, want)
	}

	options = LaunchOptions{
		URL:          "https://example.test/",
		Extraction:   ExtractionOptions{TimeoutMS: 4000},
		FrameTimeout: time.Second,
	}
	if err := validateLaunchOptions(&options); err == nil {
		t.Fatal("frame timeout below exploration unexpectedly accepted")
	}
}

func TestStrictObservationDecode(t *testing.T) {
	t.Parallel()
	valid := `{"coverageReported":false,"frames":[],"interactions":[]}`
	if _, err := decodeBatchStrict([]byte(valid)); err != nil {
		t.Fatalf("decode valid batch: %v", err)
	}
	if _, err := decodeBatchStrict([]byte(`{"coverageReported":false,"frames":[],"interactions":[],"unknown":true}`)); err == nil {
		t.Fatal("unknown observation field unexpectedly accepted")
	}
	if _, err := decodeBatchStrict([]byte(valid + `{}`)); err == nil {
		t.Fatal("trailing observation JSON unexpectedly accepted")
	}
}
