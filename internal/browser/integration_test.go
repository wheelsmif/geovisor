package browser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geo-suite/geovisor/internal/observation"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func TestLaunchObservesNestedShadowAndCrossOriginFrames(t *testing.T) {
	executable := browserExecutable(t)

	cross := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write(corpusFixture(t, "cross-frame.html"))
	}))
	defer cross.Close()
	crossURL := strings.Replace(cross.URL, "127.0.0.1", "localhost", 1)

	root := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/favicon.ico" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/html")
		name := strings.TrimPrefix(request.URL.Path, "/")
		if name == "" {
			name = "frames.html"
		}
		data := strings.ReplaceAll(string(corpusFixture(t, name)), "{{CROSS_ORIGIN}}", crossURL)
		_, _ = io.WriteString(writer, data)
	}))
	defer root.Close()

	source, err := NewLaunch(LaunchOptions{
		URL: root.URL, ExecutablePath: executable, Timeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatalf("new launch source: %v", err)
	}
	result, err := source.Observe(context.Background())
	if err != nil {
		t.Fatalf("observe launch: %v", err)
	}
	if len(result.Input.Batches) != 1 || !result.Input.Batches[0].CoverageReported {
		t.Fatalf("coverage batch = %+v", result.Input.Batches)
	}
	batch := result.Input.Batches[0]
	if len(batch.Frames) != 4 {
		t.Fatalf("frames = %d, want root, nested same-origin pair, and OOPIF: %+v", len(batch.Frames), batch.Frames)
	}
	if len(batch.Frames[0].Path) != 0 {
		t.Fatalf("first frame is not the root: %+v", batch.Frames)
	}
	assertInteraction(t, batch.Interactions, "Root action", 0)
	assertInteraction(t, batch.Interactions, "Nested frame action", 1)
	assertInteraction(t, batch.Interactions, "Recovered field Submit recovered form", 2)

	shadow := findInteraction(batch.Interactions, "Shadow action")
	if shadow == nil || len(shadow.Locators) == 0 || len(shadow.Locators[0].ShadowPath) == 0 {
		t.Fatalf("open shadow interaction has no shadow traversal locator: %+v", shadow)
	}

	assertInteraction(t, batch.Interactions, "Cross frame action", 1)
	for _, frame := range batch.Frames {
		if len(frame.Path) == 1 && frame.Path[0].Name == "cross-origin" && !frame.Accessible {
			t.Fatalf("cross-origin OOPIF was not evaluated: %s", frame.Reason)
		}
	}
}

func TestLaunchWaitsForSPAQuietAndReportsInterstitial(t *testing.T) {
	executable := browserExecutable(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/favicon.ico" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/html")
		name := strings.TrimPrefix(request.URL.Path, "/")
		if name == "" {
			name = "spa.html"
		}
		_, _ = writer.Write(corpusFixture(t, name))
	}))
	defer server.Close()

	t.Run("SPA mutation settles", func(t *testing.T) {
		source, err := NewLaunch(LaunchOptions{
			URL: server.URL + "/spa.html", ExecutablePath: executable,
			Timeout: 45 * time.Second, DOMQuietPeriod: 150 * time.Millisecond,
			DOMQuietTimeout: 2 * time.Second,
		})
		if err != nil {
			t.Fatalf("new launch source: %v", err)
		}
		result, err := source.Observe(context.Background())
		if err != nil {
			t.Fatalf("observe SPA fixture: %v", err)
		}
		assertInteraction(t, result.Input.Batches[0].Interactions, "Hydrated action", 0)
	})

	t.Run("interstitial remains nonfatal", func(t *testing.T) {
		source, err := NewLaunch(LaunchOptions{
			URL: server.URL + "/blocked.html", ExecutablePath: executable,
			Timeout: 45 * time.Second,
		})
		if err != nil {
			t.Fatalf("new launch source: %v", err)
		}
		result, err := source.Observe(context.Background())
		if err != nil {
			t.Fatalf("observe blocked fixture: %v", err)
		}
		assertInteraction(t, result.Input.Batches[0].Interactions, "Challenge action", 0)
		found := false
		for _, diagnostic := range result.Diagnostics {
			found = found || diagnostic.Code == DiagnosticAccessInterstitial
		}
		if !found {
			t.Fatalf("access interstitial diagnostic missing: %+v", result.Diagnostics)
		}
	})
}

func TestAttachPreservesBrowserAndDoesNotNavigate(t *testing.T) {
	executable := browserExecutable(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" {
			requests.Add(1)
		}
		writer.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(writer, `<!doctype html><button aria-label="Attached action">Attached</button>`)
	}))
	defer server.Close()

	process, controlURL := launchExternalBrowser(t, executable)
	defer cleanupExternalBrowser(process, controlURL)
	connection, err := connectBrowser(context.Background(), controlURL)
	if err != nil {
		t.Fatalf("connect fixture browser: %v", err)
	}
	created, err := (proto.TargetCreateTarget{URL: "about:blank"}).Call(connection.browser)
	if err != nil {
		t.Fatalf("create fixture target: %v", err)
	}
	session, err := attachTarget(context.Background(), connection.browser, created.TargetID)
	if err != nil {
		t.Fatalf("attach fixture target: %v", err)
	}
	navigated, err := (proto.PageNavigate{URL: server.URL}).Call(session)
	if err != nil {
		t.Fatalf("navigate fixture target: %v", err)
	}
	if navigated.ErrorText != "" {
		t.Fatalf("navigate fixture target: %s", navigated.ErrorText)
	}
	if err := waitForDocumentReady(context.Background(), session); err != nil {
		t.Fatalf("wait fixture target: %v", err)
	}
	detachTarget(connection.browser, session.sessionID)
	connection.closeTransport()
	before := requests.Load()

	source, err := NewAttach(AttachOptions{
		Endpoint: controlURL,
		Selector: TargetSelector{Mode: SelectExactURL, URL: server.URL},
		Timeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new attach source: %v", err)
	}
	result, err := source.Observe(context.Background())
	if err != nil {
		t.Fatalf("observe attach: %v", err)
	}
	if requests.Load() != before {
		t.Fatalf("attach observation navigated or reloaded page: requests %d -> %d", before, requests.Load())
	}
	if len(result.Input.Batches[0].Interactions) == 0 {
		t.Fatalf("attach observation returned no interactions; frames=%+v diagnostics=%+v",
			result.Input.Batches[0].Frames, result.Diagnostics)
	}
	assertInteraction(t, result.Input.Batches[0].Interactions, "Attached action", 0)

	probe, err := connectBrowser(context.Background(), controlURL)
	if err != nil {
		t.Fatalf("attach source closed existing browser: %v", err)
	}
	targets, err := (proto.TargetGetTargets{}).Call(probe.browser)
	probe.closeTransport()
	if err != nil {
		t.Fatalf("probe targets after attach: %v", err)
	}
	found := false
	for _, target := range targets.TargetInfos {
		found = found || target.TargetID == created.TargetID
	}
	if !found {
		t.Fatal("attach source closed the selected target")
	}
}

func browserExecutable(t *testing.T) string {
	t.Helper()
	executable, found := launcher.LookPath()
	if found {
		return executable
	}
	if os.Getenv("GEOVISOR_REQUIRE_BROWSER") == "1" {
		t.Fatal("GEOVISOR_REQUIRE_BROWSER=1 but no Chromium executable was found")
	}
	t.Skip("no Chromium executable found; set GEOVISOR_REQUIRE_BROWSER=1 to require browser tests")
	return ""
}

func launchExternalBrowser(t *testing.T, executable string) (*launcher.Launcher, string) {
	t.Helper()
	profile := t.TempDir()
	process := launcher.New().
		Context(context.Background()).
		Logger(io.Discard).
		Bin(executable).
		UserDataDir(filepath.Join(profile, "chromium-profile")).
		Headless(true).
		Leakless(false).
		Set("site-per-process").
		Set("disable-features", "TranslateUI").
		Delete("disable-site-isolation-trials")
	controlURL, err := process.Launch()
	if err != nil {
		t.Fatalf("launch fixture Chromium: %v", err)
	}
	return process, controlURL
}

func cleanupExternalBrowser(process *launcher.Launcher, controlURL string) {
	connection, err := connectBrowser(context.Background(), controlURL)
	if err == nil {
		_ = (proto.BrowserClose{}).Call(connection.browser)
		connection.closeTransport()
	}
	process.Kill()
	process.Cleanup()
}

func assertInteraction(
	t *testing.T,
	interactions []observation.Interaction,
	name string,
	frameDepth int,
) {
	t.Helper()
	interaction := findInteraction(interactions, name)
	if interaction == nil {
		t.Fatalf("interaction %q not found in %+v", name, interactions)
	}
	if len(interaction.FramePath) != frameDepth {
		t.Fatalf("interaction %q frame depth = %d, want %d", name, len(interaction.FramePath), frameDepth)
	}
	for _, locator := range interaction.Locators {
		if len(locator.FramePath) != frameDepth {
			t.Fatalf("interaction %q locator frame depth = %d, want %d", name, len(locator.FramePath), frameDepth)
		}
	}
}

func findInteraction(interactions []observation.Interaction, name string) *observation.Interaction {
	for index := range interactions {
		if interactions[index].Name == name {
			return &interactions[index]
		}
	}
	return nil
}

func corpusFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "corpus", filepath.Clean(name))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus fixture %q: %v", name, err)
	}
	return data
}
