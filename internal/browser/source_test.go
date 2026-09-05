package browser

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geo-suite/geovisor/internal/observation"
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
	if options.Timeout != defaultTimeout || options.DOMQuietPeriod != defaultDOMQuiet {
		t.Fatalf("timing defaults not applied: %+v", options)
	}

	invalid := options
	invalid.Endpoint = "file:///tmp/devtools"
	if err := validateAttachOptions(&invalid); err == nil {
		t.Fatal("file CDP endpoint unexpectedly accepted")
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

	selected, err := selectTarget(targets, TargetSelector{Mode: SelectActiveTopLevel}, nil)
	if err != nil {
		t.Fatalf("select fallback: %v", err)
	}
	if selected.TargetID != "a" {
		t.Fatalf("fallback target = %q, want canonical first a", selected.TargetID)
	}

	selected, err = selectTarget(
		targets,
		TargetSelector{Mode: SelectActiveTopLevel},
		map[proto.TargetTargetID]bool{"z": true},
	)
	if err != nil || selected.TargetID != "z" {
		t.Fatalf("active target = %v, %v; want z", selected, err)
	}

	selected, err = selectTarget(
		targets,
		TargetSelector{Mode: SelectExactURL, URL: "https://a.example/"},
		nil,
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
	for _, forbidden := range []string{"will", "password", "token", "secret"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("sanitized error leaked %q: %s", forbidden, rendered)
		}
	}
	if !strings.Contains(rendered, "redacted") {
		t.Fatalf("sanitized error has no redaction marker: %s", rendered)
	}
}

func TestFlattenFrameTreePreservesTreeOrder(t *testing.T) {
	t.Parallel()
	session := &sessionClient{}
	tree := &proto.PageFrameTree{
		Frame: &proto.PageFrame{ID: "root", URL: "https://root.test/"},
		ChildFrames: []*proto.PageFrameTree{
			{
				Frame: &proto.PageFrame{ID: "a", Name: "first", URL: "https://a.test/"},
				ChildFrames: []*proto.PageFrameTree{
					{Frame: &proto.PageFrame{ID: "aa", Name: "nested", URL: "https://aa.test/"}},
				},
			},
			{Frame: &proto.PageFrame{ID: "b", Name: "second", URL: "https://b.test/"}},
		},
	}
	frames := flattenFrameTree(tree, session, nil)
	if len(frames) != 4 {
		t.Fatalf("frame count = %d, want 4", len(frames))
	}
	got := []string{
		string(frames[0].frame.ID),
		string(frames[1].frame.ID),
		string(frames[2].frame.ID),
		string(frames[3].frame.ID),
	}
	want := []string{"root", "a", "aa", "b"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("frame order = %v, want %v", got, want)
		}
	}
	if len(frames[2].path) != 2 ||
		frames[2].path[0] != (observation.FrameReference{Index: 0, Name: "first", Src: "https://a.test/"}) ||
		frames[2].path[1].Index != 0 {
		t.Fatalf("nested frame path = %+v", frames[2].path)
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
