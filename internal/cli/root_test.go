package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wheelsmif/geovisor/internal/browser"
	"github.com/wheelsmif/geovisor/internal/compiler"
	"github.com/wheelsmif/geovisor/internal/emitter"
	"github.com/wheelsmif/geovisor/internal/output"
	"github.com/wheelsmif/geovisor/internal/tir"
)

type stubSource struct {
	result browser.Result
	err    error
}

func (source stubSource) Observe(context.Context) (browser.Result, error) {
	return source.result, source.err
}

type contextSource struct{}

func (contextSource) Observe(ctx context.Context) (browser.Result, error) {
	return browser.Result{}, ctx.Err()
}

type stubRegistry struct {
	emit func(context.Context, emitter.Format, *tir.Document, emitter.Options) (emitter.Result, error)
}

func (registry stubRegistry) Emit(
	ctx context.Context,
	format emitter.Format,
	document *tir.Document,
	options emitter.Options,
) (emitter.Result, error) {
	return registry.emit(ctx, format, document, options)
}

func TestHelpAndVersionKeepStdoutPure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "root help", args: []string{"--help"}, want: "Usage:"},
		{name: "inspect help", args: []string{"inspect", "--help"}, want: "--format"},
		{name: "version", args: []string{"--version"}, want: "0.0.0-dev"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if err := RunWithDependencies(
				context.Background(), test.args, &stdout, &stderr, successfulDependencies(),
			); err != nil {
				t.Fatalf("run: %v", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.want)
			}
		})
	}
}

func TestInspectRejectsInvalidModesBeforeSourceConstruction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "launch needs URL", args: []string{"inspect"}},
		{name: "launch has one URL", args: []string{"inspect", "https://one.test", "https://two.test"}},
		{name: "attach rejects positional URL", args: []string{"inspect", "https://example.test", "--cdp", "http://127.0.0.1:9222"}},
		{name: "navigate needs attach", args: []string{"inspect", "https://example.test", "--navigate", "https://other.test"}},
		{name: "all needs directory", args: []string{"inspect", "https://example.test", "--format", "all"}},
		{name: "bindings format", args: []string{"inspect", "https://example.test", "--bindings-output", "bindings.json"}},
		{name: "attach launch option", args: []string{"inspect", "--cdp", "http://127.0.0.1:9222", "--headful"}},
		{name: "invalid target", args: []string{"inspect", "--cdp", "http://127.0.0.1:9222", "--target", "first"}},
		{name: "invalid quiet timing", args: []string{"inspect", "https://example.test", "--dom-quiet", "2s", "--dom-quiet-timeout", "1s"}},
		{name: "invalid depth", args: []string{"inspect", "https://example.test", "--depth", "17"}},
		{name: "negative depth", args: []string{"inspect", "https://example.test", "--depth", "-1"}},
		{name: "frame timeout below exploration", args: []string{"inspect", "https://example.test", "--exploration-timeout", "2s", "--frame-timeout", "1s"}},
		{name: "zero frame timeout", args: []string{"inspect", "https://example.test", "--frame-timeout", "0"}},
		{name: "unsupported format", args: []string{"inspect", "https://example.test", "--format", "yaml"}},
		{name: "bindings with all", args: []string{"inspect", "https://example.test", "--format", "all", "--output", "out", "--bindings-output", "b.json"}},
		{name: "openai-strict with tir", args: []string{"inspect", "https://example.test", "--openai-strict"}},
		{name: "zero timeout", args: []string{"inspect", "https://example.test", "--timeout", "0"}},
		{name: "zero dom-quiet", args: []string{"inspect", "https://example.test", "--dom-quiet", "0"}},
		{name: "max-operations over cap", args: []string{"inspect", "https://example.test", "--max-operations", "501"}},
		{name: "zero exploration timeout", args: []string{"inspect", "https://example.test", "--exploration-timeout", "0"}},
		{name: "target requires attach", args: []string{"inspect", "https://example.test", "--target", "id:page-1"}},
		{name: "attach rejects stealth", args: []string{"inspect", "--cdp", "http://127.0.0.1:9222", "--stealth"}},
		{name: "attach rejects browser-executable", args: []string{"inspect", "--cdp", "http://127.0.0.1:9222", "--browser-executable", "chrome"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependencies := successfulDependencies()
			dependencies.NewLaunch = func(browser.LaunchOptions) (browser.BrowserSource, error) {
				t.Fatal("launch source constructed for invalid invocation")
				return nil, nil
			}
			dependencies.NewAttach = func(browser.AttachOptions) (browser.BrowserSource, error) {
				t.Fatal("attach source constructed for invalid invocation")
				return nil, nil
			}
			err := RunWithDependencies(
				context.Background(), test.args, &bytes.Buffer{}, &bytes.Buffer{}, dependencies,
			)
			if ExitCode(err) != ExitUsage {
				t.Fatalf("error = %v, exit = %d, want usage %d", err, ExitCode(err), ExitUsage)
			}
		})
	}
}

func TestInspectBuildsLaunchAndAttachSources(t *testing.T) {
	t.Parallel()
	t.Run("launch", func(t *testing.T) {
		var got browser.LaunchOptions
		dependencies := successfulDependencies()
		dependencies.NewLaunch = func(options browser.LaunchOptions) (browser.BrowserSource, error) {
			got = options
			return stubSource{}, nil
		}
		var stdout bytes.Buffer
		err := RunWithDependencies(context.Background(), []string{
			"inspect", "https://example.test/path",
			"--headful", "--stealth", "--browser-executable", "chromium",
			"--safe-explore", "--depth", "4", "--max-operations", "7",
			"--exploration-timeout", "1500ms",
		}, &stdout, &bytes.Buffer{}, dependencies)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if got.URL != "https://example.test/path" || got.Headless == nil || *got.Headless {
			t.Fatalf("launch options = %+v", got)
		}
		if !got.Stealth || got.ExecutablePath != "chromium" ||
			!got.Extraction.SafeExplore || got.Extraction.MaxDepth != 4 ||
			got.Extraction.MaxOperations != 7 || got.Extraction.TimeoutMS != 1500 ||
			got.FrameTimeout != 0 {
			t.Fatalf("launch extraction/options = %+v", got)
		}
	})

	t.Run("launch with explicit frame timeout", func(t *testing.T) {
		var got browser.LaunchOptions
		dependencies := successfulDependencies()
		dependencies.NewLaunch = func(options browser.LaunchOptions) (browser.BrowserSource, error) {
			got = options
			return stubSource{}, nil
		}
		err := RunWithDependencies(context.Background(), []string{
			"inspect", "https://example.test/path",
			"--exploration-timeout", "1500ms", "--frame-timeout", "8s",
		}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if got.FrameTimeout != 8*time.Second || got.Extraction.TimeoutMS != 1500 {
			t.Fatalf("frame timeout options = %+v", got)
		}
	})

	t.Run("attach without implicit navigation", func(t *testing.T) {
		var got browser.AttachOptions
		dependencies := successfulDependencies()
		dependencies.NewAttach = func(options browser.AttachOptions) (browser.BrowserSource, error) {
			got = options
			return stubSource{}, nil
		}
		err := RunWithDependencies(context.Background(), []string{
			"inspect", "--cdp", "http://127.0.0.1:9222", "--target", "id:page-1",
		}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if got.RequestedURL != "" || got.Selector.Mode != browser.SelectExactTargetID ||
			got.Selector.TargetID != "page-1" {
			t.Fatalf("attach options = %+v", got)
		}
	})

	t.Run("attach explicit navigation", func(t *testing.T) {
		var got browser.AttachOptions
		dependencies := successfulDependencies()
		dependencies.NewAttach = func(options browser.AttachOptions) (browser.BrowserSource, error) {
			got = options
			return stubSource{}, nil
		}
		err := RunWithDependencies(context.Background(), []string{
			"inspect", "--cdp", "ws://127.0.0.1/devtools/browser/id",
			"--navigate", "https://example.test/next",
		}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if got.RequestedURL != "https://example.test/next" {
			t.Fatalf("requested URL = %q", got.RequestedURL)
		}
	})
}

func TestParseTargetSelector(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value    string
		mode     browser.TargetSelectionMode
		targetID string
		url      string
		wantErr  bool
	}{
		{value: "active", mode: browser.SelectActiveTopLevel},
		{value: "id: page-1 ", mode: browser.SelectExactTargetID, targetID: "page-1"},
		{value: "url:https://example.test/path", mode: browser.SelectExactURL, url: "https://example.test/path"},
		{value: "id:", wantErr: true},
		{value: "url:javascript:alert(1)", wantErr: true},
		{value: "url:https://user:pass@example.test", wantErr: true},
		{value: "focused", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTargetSelector(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseTargetSelector(%q) error = %v, wantErr %v", test.value, err, test.wantErr)
			}
			if err == nil && (got.Mode != test.mode || got.TargetID != test.targetID || got.URL != test.url) {
				t.Fatalf("selector = %+v", got)
			}
		})
	}
}

func TestAllOutputUsesEmitterNamesAndStagesBeforeWriting(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	var formats []emitter.Format
	var openAIStrict bool
	dependencies := successfulDependencies()
	dependencies.Registry = stubRegistry{emit: func(
		_ context.Context,
		format emitter.Format,
		_ *tir.Document,
		options emitter.Options,
	) (emitter.Result, error) {
		formats = append(formats, format)
		if format == emitter.FormatOpenAI {
			openAIStrict = options.Strict
		}
		result := emitter.Result{Primary: emitter.Artifact{
			Name: "primary-" + string(format), Data: []byte("primary:" + string(format)),
		}}
		if format == emitter.FormatMCP || format == emitter.FormatOpenAI {
			result.Companion = &emitter.Artifact{
				Name: "bindings-" + string(format), Data: []byte("bindings:" + string(format)),
			}
		}
		return result, nil
	}}
	var stdout bytes.Buffer
	err := RunWithDependencies(context.Background(), []string{
		"inspect", "https://example.test", "--format", "all",
		"--output", directory, "--openai-strict",
	}, &stdout, &bytes.Buffer{}, dependencies)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	wantFormats := []emitter.Format{
		emitter.FormatTIRJSON, emitter.FormatWebMCP, emitter.FormatMCP, emitter.FormatOpenAI,
	}
	if strings.Join(formatStrings(formats), ",") != strings.Join(formatStrings(wantFormats), ",") {
		t.Fatalf("formats = %v, want %v", formats, wantFormats)
	}
	if !openAIStrict {
		t.Fatal("OpenAI strict option was not forwarded")
	}
	for _, name := range []string{
		"primary-tir-json", "primary-webmcp", "primary-mcp", "bindings-mcp",
		"primary-openai", "bindings-openai",
	} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("artifact %q: %v", name, err)
		}
	}
}

func TestAllOutputIncludesBuiltInCompanions(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	dependencies := successfulDependencies()
	dependencies.Registry = emitter.DefaultRegistry()
	err := RunWithDependencies(context.Background(), []string{
		"inspect", "https://example.test", "--format", "all", "--output", directory,
	}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{
		"tir.json",
		"tools.webmcp.js",
		"tools.mcp.json",
		"tools.mcp.bindings.json",
		"tools.openai.json",
		"tools.openai.bindings.json",
	} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("built-in artifact %q: %v", name, err)
		}
	}
}

func TestSingleOutputCompanionBehavior(t *testing.T) {
	t.Parallel()
	newDependencies := func() Dependencies {
		dependencies := successfulDependencies()
		dependencies.Registry = stubRegistry{emit: func(
			context.Context, emitter.Format, *tir.Document, emitter.Options,
		) (emitter.Result, error) {
			return emitter.Result{
				Primary: emitter.Artifact{Name: "tools.mcp.json", Data: []byte("primary")},
				Companion: &emitter.Artifact{
					Name: "tools.mcp.bindings.json", Data: []byte("bindings"),
				},
			}, nil
		}}
		return dependencies
	}

	t.Run("stdout warns when bindings omitted", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := RunWithDependencies(context.Background(), []string{
			"inspect", "https://example.test", "--format", "mcp",
		}, &stdout, &stderr, newDependencies())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if stdout.String() != "primary" || !strings.Contains(stderr.String(), "not persisted") {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})

	t.Run("primary file gets adjacent bindings", func(t *testing.T) {
		directory := t.TempDir()
		primary := filepath.Join(directory, "custom.json")
		err := RunWithDependencies(context.Background(), []string{
			"inspect", "https://example.test", "--format", "mcp", "--output", primary,
		}, &bytes.Buffer{}, &bytes.Buffer{}, newDependencies())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		assertFileData(t, primary, "primary")
		assertFileData(t, filepath.Join(directory, "tools.mcp.bindings.json"), "bindings")
	})

	t.Run("explicit bindings with stdout primary", func(t *testing.T) {
		bindings := filepath.Join(t.TempDir(), "recipes.json")
		var stdout, stderr bytes.Buffer
		err := RunWithDependencies(context.Background(), []string{
			"inspect", "https://example.test", "--format", "mcp",
			"--bindings-output", bindings,
		}, &stdout, &stderr, newDependencies())
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		assertFileData(t, bindings, "bindings")
		if stdout.String() != "primary" || stderr.Len() != 0 {
			t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})
}

func TestEmitterFailureDoesNotWriteAnyAllOutput(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	existing := filepath.Join(directory, "primary-tir-json")
	if err := os.WriteFile(existing, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	dependencies := successfulDependencies()
	dependencies.Registry = stubRegistry{emit: func(
		_ context.Context,
		format emitter.Format,
		_ *tir.Document,
		_ emitter.Options,
	) (emitter.Result, error) {
		if format == emitter.FormatWebMCP {
			return emitter.Result{}, &emitter.Error{
				Format: format, Code: emitter.CodeMarshal, Err: errors.New("failed"),
			}
		}
		return emitter.Result{Primary: emitter.Artifact{
			Name: "primary-" + string(format), Data: []byte("replacement"),
		}}, nil
	}}
	writes := 0
	dependencies.WriteFiles = func(context.Context, []output.File) error {
		writes++
		return nil
	}
	err := RunWithDependencies(context.Background(), []string{
		"inspect", "https://example.test", "--format", "all", "--output", directory,
	}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
	if ExitCode(err) != ExitEmitter {
		t.Fatalf("error = %v, exit = %d", err, ExitCode(err))
	}
	if writes != 0 {
		t.Fatalf("writer called %d times before all emitters succeeded", writes)
	}
	assertFileData(t, existing, "original")
}

func TestAllOutputRejectsEmitterNameCollisions(t *testing.T) {
	t.Parallel()
	dependencies := successfulDependencies()
	dependencies.Registry = stubRegistry{emit: func(
		context.Context, emitter.Format, *tir.Document, emitter.Options,
	) (emitter.Result, error) {
		return emitter.Result{Primary: emitter.Artifact{
			Name: "duplicate.json", Data: []byte("artifact"),
		}}, nil
	}}
	writes := 0
	dependencies.WriteFiles = func(context.Context, []output.File) error {
		writes++
		return nil
	}
	err := RunWithDependencies(context.Background(), []string{
		"inspect", "https://example.test", "--format", "all", "--output", t.TempDir(),
	}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
	if ExitCode(err) != ExitOutput || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("error = %v, exit = %d", err, ExitCode(err))
	}
	if writes != 0 {
		t.Fatalf("writer called %d times for colliding emitter names", writes)
	}
}

func TestDiagnosticsRemainNonfatalAndTyped(t *testing.T) {
	t.Parallel()
	dependencies := successfulDependencies()
	dependencies.NewLaunch = func(browser.LaunchOptions) (browser.BrowserSource, error) {
		return stubSource{result: browser.Result{Diagnostics: []browser.Diagnostic{{
			Code: browser.DiagnosticFrameUncovered, Severity: browser.SeverityWarning,
			Message: "frame inaccessible",
		}}}}, nil
	}
	var stdout, stderr bytes.Buffer
	err := RunWithDependencies(context.Background(), []string{
		"inspect", "https://example.test",
	}, &stdout, &stderr, dependencies)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != "artifact" ||
		!strings.Contains(stderr.String(), string(browser.DiagnosticFrameUncovered)) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestUnknownCommandIsUsage(t *testing.T) {
	t.Parallel()
	err := RunWithDependencies(
		context.Background(), []string{"frobnicate"}, &bytes.Buffer{}, &bytes.Buffer{}, successfulDependencies(),
	)
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error = %v, exit = %d", err, ExitCode(err))
	}
}

func TestOpenAIStrictAppliesOnlyToOpenAIWhenEmittingAll(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	strict := map[emitter.Format]bool{}
	dependencies := successfulDependencies()
	dependencies.Registry = stubRegistry{emit: func(
		_ context.Context,
		format emitter.Format,
		_ *tir.Document,
		options emitter.Options,
	) (emitter.Result, error) {
		strict[format] = options.Strict
		return emitter.Result{Primary: emitter.Artifact{
			Name: "primary-" + string(format) + ".json", Data: []byte("ok"),
		}}, nil
	}}
	var stderr bytes.Buffer
	err := RunWithDependencies(context.Background(), []string{
		"inspect", "https://example.test",
		"--format", "all", "--output", directory, "--openai-strict",
	}, &bytes.Buffer{}, &stderr, dependencies)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strict[emitter.FormatOpenAI] {
		t.Fatal("OpenAI emit did not receive Strict")
	}
	if strict[emitter.FormatMCP] || strict[emitter.FormatWebMCP] || strict[emitter.FormatTIRJSON] {
		t.Fatalf("Strict leaked to non-OpenAI formats: %#v", strict)
	}
	if !strings.Contains(stderr.String(), "--openai-strict applies only to the OpenAI artifact") {
		t.Fatalf("stderr = %q, want openai-strict note", stderr.String())
	}
}

func TestValidateDependenciesRejectsEachNilField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Dependencies)
		want   string
	}{
		{name: "launch", mutate: func(d *Dependencies) { d.NewLaunch = nil }, want: "launch dependency"},
		{name: "attach", mutate: func(d *Dependencies) { d.NewAttach = nil }, want: "attach dependency"},
		{name: "compile", mutate: func(d *Dependencies) { d.Compile = nil }, want: "compiler dependency"},
		{name: "registry", mutate: func(d *Dependencies) { d.Registry = nil }, want: "emitter registry"},
		{name: "write files", mutate: func(d *Dependencies) { d.WriteFiles = nil }, want: "output dependency"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependencies := successfulDependencies()
			test.mutate(&dependencies)
			err := validateDependencies(dependencies)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExitGenericFallbackAndNilDependencies(t *testing.T) {
	t.Parallel()
	if got := ExitCode(errors.New("plain failure")); got != ExitGeneric {
		t.Fatalf("ExitCode(plain) = %d, want %d", got, ExitGeneric)
	}
	err := RunWithDependencies(
		context.Background(), []string{"inspect", "https://example.test"},
		&bytes.Buffer{}, &bytes.Buffer{}, Dependencies{},
	)
	if ExitCode(err) != ExitGeneric || !strings.Contains(err.Error(), "launch dependency") {
		t.Fatalf("nil dependencies error = %v, exit = %d", err, ExitCode(err))
	}
}

func TestInvalidBrowserConfigurationIsUsage(t *testing.T) {
	t.Parallel()
	dependencies := successfulDependencies()
	dependencies.NewLaunch = func(browser.LaunchOptions) (browser.BrowserSource, error) {
		return nil, &browser.Error{
			Code: browser.ErrorInvalidConfiguration, Message: "URL must be HTTP(S)",
		}
	}
	err := RunWithDependencies(
		context.Background(), []string{"inspect", "https://example.test"},
		&bytes.Buffer{}, &bytes.Buffer{}, dependencies,
	)
	if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "URL must be HTTP(S)") {
		t.Fatalf("error = %v, exit = %d", err, ExitCode(err))
	}
}

func TestValidateArtifactName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "foo/bar.json", `foo\bar.json`, "../escape.json"} {
		if err := validateArtifactName(name); err == nil {
			t.Errorf("validateArtifactName(%q) succeeded", name)
		}
	}
	if err := validateArtifactName("tools.mcp.json"); err != nil {
		t.Fatalf("safe name rejected: %v", err)
	}
}

func TestAllOutputRejectsUnsafeArtifactNames(t *testing.T) {
	t.Parallel()
	dependencies := successfulDependencies()
	dependencies.Registry = stubRegistry{emit: func(
		context.Context, emitter.Format, *tir.Document, emitter.Options,
	) (emitter.Result, error) {
		return emitter.Result{Primary: emitter.Artifact{
			Name: "../escape.json", Data: []byte("artifact"),
		}}, nil
	}}
	writes := 0
	dependencies.WriteFiles = func(context.Context, []output.File) error {
		writes++
		return nil
	}
	err := RunWithDependencies(context.Background(), []string{
		"inspect", "https://example.test", "--format", "all", "--output", t.TempDir(),
	}, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
	if ExitCode(err) != ExitOutput || !strings.Contains(err.Error(), "unsafe emitter artifact name") {
		t.Fatalf("error = %v, exit = %d", err, ExitCode(err))
	}
	if writes != 0 {
		t.Fatalf("writer called %d times for an unsafe artifact name", writes)
	}
}

func TestWriteBytesReportsShortWrite(t *testing.T) {
	t.Parallel()
	err := writeBytes(stallWriter{}, []byte("artifact"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

func TestInspectStdoutShortWriteIsOutputFailure(t *testing.T) {
	t.Parallel()
	err := RunWithDependencies(
		context.Background(), []string{"inspect", "https://example.test"},
		stallWriter{}, &bytes.Buffer{}, successfulDependencies(),
	)
	if ExitCode(err) != ExitOutput || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, exit = %d", err, ExitCode(err))
	}
}

func TestExitCodeMappingAndCancellation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		code int
		edit func(*Dependencies)
		args []string
		ctx  func() context.Context
	}{
		{
			name: "browser", code: ExitBrowser,
			edit: func(dependencies *Dependencies) {
				dependencies.NewLaunch = func(browser.LaunchOptions) (browser.BrowserSource, error) {
					return stubSource{err: &browser.Error{
						Code: browser.ErrorConnect, Message: "connect failed",
					}}, nil
				}
			},
		},
		{
			name: "compile", code: ExitCompile,
			edit: func(dependencies *Dependencies) {
				dependencies.Compile = func(context.Context, compiler.Input) (*tir.Document, error) {
					return nil, &compiler.Error{Field: "input", Code: "bad", Message: "bad input"}
				}
			},
		},
		{
			name: "emitter", code: ExitEmitter,
			edit: func(dependencies *Dependencies) {
				dependencies.Registry = stubRegistry{emit: func(
					context.Context, emitter.Format, *tir.Document, emitter.Options,
				) (emitter.Result, error) {
					return emitter.Result{}, &emitter.Error{
						Format: emitter.FormatTIRJSON, Code: emitter.CodeMarshal, Err: errors.New("bad"),
					}
				}}
			},
		},
		{
			name: "output", code: ExitOutput, args: []string{"--output", "artifact.json"},
			edit: func(dependencies *Dependencies) {
				dependencies.WriteFiles = func(context.Context, []output.File) error {
					return errors.New("disk full")
				}
			},
		},
		{
			name: "canceled", code: ExitCanceled,
			edit: func(dependencies *Dependencies) {
				dependencies.NewLaunch = func(browser.LaunchOptions) (browser.BrowserSource, error) {
					return contextSource{}, nil
				}
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dependencies := successfulDependencies()
			test.edit(&dependencies)
			ctx := context.Background()
			if test.ctx != nil {
				ctx = test.ctx()
			}
			args := append([]string{"inspect", "https://example.test"}, test.args...)
			err := RunWithDependencies(ctx, args, &bytes.Buffer{}, &bytes.Buffer{}, dependencies)
			if ExitCode(err) != test.code {
				t.Fatalf("error = %v, exit = %d, want %d", err, ExitCode(err), test.code)
			}
		})
	}
}

func successfulDependencies() Dependencies {
	return Dependencies{
		NewLaunch: func(browser.LaunchOptions) (browser.BrowserSource, error) {
			return stubSource{}, nil
		},
		NewAttach: func(browser.AttachOptions) (browser.BrowserSource, error) {
			return stubSource{}, nil
		},
		Compile: func(context.Context, compiler.Input) (*tir.Document, error) {
			return tir.NewDocument(tir.SourceMetadata{
				Kind: tir.SourceLaunchURL, ExecutionBoundary: tir.ExecutionAgentOwned,
			}), nil
		},
		Registry: stubRegistry{emit: func(
			context.Context, emitter.Format, *tir.Document, emitter.Options,
		) (emitter.Result, error) {
			return emitter.Result{Primary: emitter.Artifact{
				Name: "artifact.json", Data: []byte("artifact"),
			}}, nil
		}},
		WriteFiles: output.WriteFiles,
	}
}

func formatStrings(formats []emitter.Format) []string {
	result := make([]string, len(formats))
	for index, format := range formats {
		result[index] = string(format)
	}
	return result
}

type stallWriter struct{}

func (stallWriter) Write([]byte) (int, error) {
	return 0, nil
}

func assertFileData(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%q = %q, want %q", path, data, want)
	}
}
