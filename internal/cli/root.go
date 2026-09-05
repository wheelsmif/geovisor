// Package cli provides the geovisor command-line entry point.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/geo-suite/geovisor/internal/browser"
	"github.com/geo-suite/geovisor/internal/compiler"
	"github.com/geo-suite/geovisor/internal/emitter"
	"github.com/geo-suite/geovisor/internal/output"
	"github.com/geo-suite/geovisor/internal/tir"
	"github.com/geo-suite/geovisor/internal/version"
	"github.com/spf13/cobra"
)

const (
	defaultTimeout            = 30 * time.Second
	defaultDOMQuiet           = 250 * time.Millisecond
	defaultDOMQuietTimeout    = 2 * time.Second
	defaultDepth              = 3
	defaultMaxOperations      = 20
	defaultExplorationTimeout = time.Second
	maxDepth                  = 16
	maxOperations             = 500
	maxExplorationTimeout     = 10 * time.Second
)

// Registry is the emitter boundary consumed by the CLI.
type Registry interface {
	Emit(context.Context, emitter.Format, *tir.Document, emitter.Options) (emitter.Result, error)
}

// Dependencies are injectable process boundaries used by browser-free tests.
type Dependencies struct {
	NewLaunch  func(browser.LaunchOptions) (browser.BrowserSource, error)
	NewAttach  func(browser.AttachOptions) (browser.BrowserSource, error)
	Compile    func(compiler.Input) (*tir.Document, error)
	Registry   Registry
	WriteFiles func([]output.File) error
}

type inspectOptions struct {
	Format             string
	Output             string
	BindingsOutput     string
	OpenAIStrict       bool
	Timeout            time.Duration
	DOMQuiet           time.Duration
	DOMQuietTimeout    time.Duration
	Depth              int
	MaxOperations      int
	ExplorationTimeout time.Duration
	SafeExplore        bool
	Headful            bool
	Stealth            bool
	BrowserExecutable  string
	CDP                string
	Target             string
	Navigate           string
	URL                string
	selectedFormat     emitter.Format
	all                bool
	selector           browser.TargetSelector
}

// DefaultDependencies returns the production browser/compiler/emitter pipeline.
func DefaultDependencies() Dependencies {
	return Dependencies{
		NewLaunch:  browser.NewLaunch,
		NewAttach:  browser.NewAttach,
		Compile:    compiler.Compile,
		Registry:   emitter.DefaultRegistry(),
		WriteFiles: output.WriteFiles,
	}
}

// Run executes the CLI with a background context.
func Run(args []string, stdout, stderr io.Writer) error {
	return RunContext(context.Background(), args, stdout, stderr)
}

// RunContext executes the production CLI with caller-owned cancellation.
func RunContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return RunWithDependencies(ctx, args, stdout, stderr, DefaultDependencies())
}

// RunWithDependencies executes the CLI with explicit process boundaries.
func RunWithDependencies(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	dependencies Dependencies,
) error {
	if ctx == nil {
		return errors.New("CLI context must not be nil")
	}
	if stdout == nil || stderr == nil {
		return errors.New("CLI writers must not be nil")
	}
	if err := validateDependencies(dependencies); err != nil {
		return err
	}

	root := newRootCommand(stdout, stderr, dependencies)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return nil
	}
	return err
}

func validateDependencies(dependencies Dependencies) error {
	switch {
	case dependencies.NewLaunch == nil:
		return errors.New("CLI launch dependency must not be nil")
	case dependencies.NewAttach == nil:
		return errors.New("CLI attach dependency must not be nil")
	case dependencies.Compile == nil:
		return errors.New("CLI compiler dependency must not be nil")
	case dependencies.Registry == nil:
		return errors.New("CLI emitter registry must not be nil")
	case dependencies.WriteFiles == nil:
		return errors.New("CLI output dependency must not be nil")
	default:
		return nil
	}
}

func newRootCommand(stdout, stderr io.Writer, dependencies Dependencies) *cobra.Command {
	command := &cobra.Command{
		Use:           "geovisor",
		Short:         "Deterministic browser-to-contract tooling",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version.Version,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			return usagef("unknown command %q", args[0])
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.SetOut(stderr)
	command.SetErr(stderr)
	command.SetVersionTemplate("{{.Version}}\n")
	command.CompletionOptions.DisableDefaultCmd = true
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &UsageError{Message: err.Error()}
	})
	command.AddCommand(newInspectCommand(stdout, stderr, dependencies))
	return command
}

func newInspectCommand(stdout, stderr io.Writer, dependencies Dependencies) *cobra.Command {
	options := inspectOptions{
		Format:             "tir",
		Timeout:            defaultTimeout,
		DOMQuiet:           defaultDOMQuiet,
		DOMQuietTimeout:    defaultDOMQuietTimeout,
		Depth:              defaultDepth,
		MaxOperations:      defaultMaxOperations,
		ExplorationTimeout: defaultExplorationTimeout,
		Target:             "active",
	}

	command := &cobra.Command{
		Use:   "inspect <url>",
		Short: "Observe a browser page and emit tool artifacts",
		Long: "Launch isolated Chromium for a URL, or attach to an existing CDP endpoint.\n" +
			"Attach mode does not navigate unless --navigate is explicitly supplied.",
		Args: func(command *cobra.Command, args []string) error {
			return validateInspectInvocation(command, args, &options)
		},
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 1 {
				options.URL = args[0]
			}
			return executeInspect(command.Context(), options, stdout, stderr, dependencies)
		},
	}

	flags := command.Flags()
	flags.StringVarP(&options.Format, "format", "f", options.Format, "output format: tir, webmcp, mcp, openai, or all")
	flags.StringVarP(&options.Output, "output", "o", "", "output file, or output directory with --format all")
	flags.StringVar(&options.BindingsOutput, "bindings-output", "", "MCP/OpenAI executable bindings file")
	flags.BoolVar(&options.OpenAIStrict, "openai-strict", false, "emit strict OpenAI function schemas")
	flags.DurationVar(&options.Timeout, "timeout", options.Timeout, "overall browser operation timeout")
	flags.DurationVar(&options.DOMQuiet, "dom-quiet", options.DOMQuiet, "required DOM quiet period")
	flags.DurationVar(&options.DOMQuietTimeout, "dom-quiet-timeout", options.DOMQuietTimeout, "maximum DOM quiet wait")
	flags.IntVar(&options.Depth, "depth", options.Depth, "safe exploration depth (0-16)")
	flags.IntVar(&options.MaxOperations, "max-operations", options.MaxOperations, "safe exploration operation limit (0-500)")
	flags.DurationVar(&options.ExplorationTimeout, "exploration-timeout", options.ExplorationTimeout, "safe exploration time limit")
	flags.BoolVar(&options.SafeExplore, "safe-explore", false, "enable non-navigating, non-submitting safe exploration")
	flags.BoolVar(&options.Headful, "headful", false, "show launched Chromium")
	flags.BoolVar(&options.Stealth, "stealth", false, "opt in to limited automation-marker removal")
	flags.StringVar(&options.BrowserExecutable, "browser-executable", "", "Chromium executable for launch mode")
	flags.StringVar(&options.CDP, "cdp", "", "existing Chromium HTTP(S) or WebSocket CDP endpoint")
	flags.StringVar(&options.Target, "target", options.Target, "attach target: active, id:<id>, or url:<exact-url>")
	flags.StringVar(&options.Navigate, "navigate", "", "explicit HTTP(S) navigation URL in attach mode")
	return command
}

func validateInspectInvocation(command *cobra.Command, args []string, options *inspectOptions) error {
	if options.CDP == "" {
		if len(args) != 1 {
			return usagef("launch mode requires exactly one HTTP(S) URL")
		}
		if options.Navigate != "" {
			return usagef("--navigate requires --cdp")
		}
		if command.Flags().Changed("target") {
			return usagef("--target requires --cdp")
		}
	} else {
		if len(args) != 0 {
			return usagef("attach mode accepts no positional URL; use --navigate for explicit navigation")
		}
		if command.Flags().Changed("headful") {
			return usagef("--headful is available only in launch mode")
		}
		if command.Flags().Changed("stealth") {
			return usagef("--stealth is available only in launch mode")
		}
		if command.Flags().Changed("browser-executable") {
			return usagef("--browser-executable is available only in launch mode")
		}
		selector, err := ParseTargetSelector(options.Target)
		if err != nil {
			return err
		}
		options.selector = selector
	}

	switch options.Format {
	case "tir":
		options.selectedFormat = emitter.FormatTIRJSON
	case "webmcp":
		options.selectedFormat = emitter.FormatWebMCP
	case "mcp":
		options.selectedFormat = emitter.FormatMCP
	case "openai":
		options.selectedFormat = emitter.FormatOpenAI
	case "all":
		options.all = true
	default:
		return usagef("unsupported --format %q (want tir, webmcp, mcp, openai, or all)", options.Format)
	}

	if options.all && strings.TrimSpace(options.Output) == "" {
		return usagef("--format all requires --output directory")
	}
	if options.all && options.BindingsOutput != "" {
		return usagef("--bindings-output is not valid with --format all")
	}
	if options.BindingsOutput != "" &&
		options.selectedFormat != emitter.FormatMCP &&
		options.selectedFormat != emitter.FormatOpenAI {
		return usagef("--bindings-output is available only for mcp or openai")
	}
	if options.OpenAIStrict && !options.all && options.selectedFormat != emitter.FormatOpenAI {
		return usagef("--openai-strict is available only for openai or all")
	}
	if options.Timeout <= 0 {
		return usagef("--timeout must be positive")
	}
	if options.DOMQuiet <= 0 {
		return usagef("--dom-quiet must be positive")
	}
	if options.DOMQuietTimeout < options.DOMQuiet {
		return usagef("--dom-quiet-timeout must be at least --dom-quiet")
	}
	if options.Depth < 0 || options.Depth > maxDepth {
		return usagef("--depth must be between 0 and %d", maxDepth)
	}
	if options.MaxOperations < 0 || options.MaxOperations > maxOperations {
		return usagef("--max-operations must be between 0 and %d", maxOperations)
	}
	if options.ExplorationTimeout <= 0 || options.ExplorationTimeout > maxExplorationTimeout {
		return usagef("--exploration-timeout must be positive and at most %s", maxExplorationTimeout)
	}
	return nil
}

// ParseTargetSelector parses the explicit attach-target syntax.
func ParseTargetSelector(value string) (browser.TargetSelector, error) {
	switch {
	case value == "active":
		return browser.TargetSelector{Mode: browser.SelectActiveTopLevel}, nil
	case strings.HasPrefix(value, "id:"):
		id := strings.TrimSpace(strings.TrimPrefix(value, "id:"))
		if id == "" {
			return browser.TargetSelector{}, usagef("--target id requires a non-empty target ID")
		}
		return browser.TargetSelector{Mode: browser.SelectExactTargetID, TargetID: id}, nil
	case strings.HasPrefix(value, "url:"):
		raw := strings.TrimPrefix(value, "url:")
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host == "" || parsed.User != nil {
			return browser.TargetSelector{}, usagef("--target URL must be an exact HTTP(S) URL without credentials")
		}
		return browser.TargetSelector{Mode: browser.SelectExactURL, URL: raw}, nil
	default:
		return browser.TargetSelector{}, usagef("--target must be active, id:<id>, or url:<exact-url>")
	}
}

func executeInspect(
	ctx context.Context,
	options inspectOptions,
	stdout, stderr io.Writer,
	dependencies Dependencies,
) error {
	source, err := makeSource(options, dependencies)
	if err != nil {
		var browserErr *browser.Error
		if errors.As(err, &browserErr) && browserErr.Code == browser.ErrorInvalidConfiguration {
			return &UsageError{Message: err.Error()}
		}
		return classify(ExitBrowser, err)
	}

	result, err := source.Observe(ctx)
	if err != nil {
		return classify(ExitBrowser, err)
	}
	if err := writeDiagnostics(stderr, result.Diagnostics); err != nil {
		return classify(ExitOutput, err)
	}

	document, err := dependencies.Compile(result.Input)
	if err != nil {
		return classify(ExitCompile, fmt.Errorf("compile observation: %w", err))
	}

	formats := []emitter.Format{options.selectedFormat}
	if options.all {
		formats = []emitter.Format{
			emitter.FormatTIRJSON,
			emitter.FormatWebMCP,
			emitter.FormatMCP,
			emitter.FormatOpenAI,
		}
	}
	results := make([]emitter.Result, 0, len(formats))
	for _, format := range formats {
		emitted, emitErr := dependencies.Registry.Emit(
			ctx,
			format,
			document,
			emitter.Options{Strict: options.OpenAIStrict && format == emitter.FormatOpenAI},
		)
		if emitErr != nil {
			return classify(ExitEmitter, emitErr)
		}
		results = append(results, emitted)
	}

	if options.all {
		files, err := allOutputFiles(options.Output, results)
		if err != nil {
			return classify(ExitOutput, err)
		}
		if err := dependencies.WriteFiles(files); err != nil {
			return classify(ExitOutput, err)
		}
		return nil
	}
	return writeSingleOutput(stdout, stderr, options, results[0], dependencies.WriteFiles)
}

func makeSource(options inspectOptions, dependencies Dependencies) (browser.BrowserSource, error) {
	extraction := browser.ExtractionOptions{
		SafeExplore:   options.SafeExplore,
		MaxDepth:      options.Depth,
		MaxOperations: options.MaxOperations,
		TimeoutMS:     int(options.ExplorationTimeout / time.Millisecond),
	}
	if options.CDP != "" {
		return dependencies.NewAttach(browser.AttachOptions{
			Endpoint:        options.CDP,
			RequestedURL:    options.Navigate,
			Selector:        options.selector,
			Timeout:         options.Timeout,
			DOMQuietPeriod:  options.DOMQuiet,
			DOMQuietTimeout: options.DOMQuietTimeout,
			Extraction:      extraction,
		})
	}
	headless := !options.Headful
	return dependencies.NewLaunch(browser.LaunchOptions{
		URL:             options.URL,
		ExecutablePath:  options.BrowserExecutable,
		Headless:        &headless,
		Timeout:         options.Timeout,
		DOMQuietPeriod:  options.DOMQuiet,
		DOMQuietTimeout: options.DOMQuietTimeout,
		Stealth:         options.Stealth,
		Extraction:      extraction,
	})
}

func writeDiagnostics(writer io.Writer, diagnostics []browser.Diagnostic) error {
	for _, diagnostic := range diagnostics {
		if _, err := fmt.Fprintf(
			writer,
			"browser %s [%s]: %s (frame depth %d)\n",
			diagnostic.Severity,
			diagnostic.Code,
			diagnostic.Message,
			len(diagnostic.FramePath),
		); err != nil {
			return fmt.Errorf("write browser diagnostic: %w", err)
		}
	}
	return nil
}

func allOutputFiles(directory string, results []emitter.Result) ([]output.File, error) {
	files := make([]output.File, 0, len(results)*2)
	for _, result := range results {
		artifacts := []emitter.Artifact{result.Primary}
		if result.Companion != nil {
			artifacts = append(artifacts, *result.Companion)
		}
		for _, artifact := range artifacts {
			if err := validateArtifactName(artifact.Name); err != nil {
				return nil, err
			}
			files = append(files, output.File{
				Path: filepath.Join(directory, artifact.Name),
				Data: artifact.Data,
			})
		}
	}
	if err := validateOutputCollisions(files); err != nil {
		return nil, err
	}
	return files, nil
}

func writeSingleOutput(
	stdout, stderr io.Writer,
	options inspectOptions,
	result emitter.Result,
	writeFiles func([]output.File) error,
) error {
	files := make([]output.File, 0, 2)
	if options.Output != "" {
		files = append(files, output.File{Path: options.Output, Data: result.Primary.Data})
	}

	if result.Companion != nil {
		bindingsPath := options.BindingsOutput
		if bindingsPath == "" && options.Output != "" {
			if err := validateArtifactName(result.Companion.Name); err != nil {
				return classify(ExitOutput, err)
			}
			bindingsPath = filepath.Join(filepath.Dir(options.Output), result.Companion.Name)
		}
		if bindingsPath != "" {
			files = append(files, output.File{Path: bindingsPath, Data: result.Companion.Data})
		} else {
			if _, err := fmt.Fprintln(
				stderr,
				"warning: executable bindings were not persisted; use --bindings-output",
			); err != nil {
				return classify(ExitOutput, fmt.Errorf("write bindings warning: %w", err))
			}
		}
	}

	if err := validateOutputCollisions(files); err != nil {
		return classify(ExitOutput, err)
	}
	if len(files) > 0 {
		if err := writeFiles(files); err != nil {
			return classify(ExitOutput, err)
		}
	}
	if options.Output == "" {
		if err := writeBytes(stdout, result.Primary.Data); err != nil {
			return classify(ExitOutput, fmt.Errorf("write primary artifact: %w", err))
		}
	}
	return nil
}

func validateArtifactName(name string) error {
	if name == "" || name == "." || filepath.Base(name) != name ||
		strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("unsafe emitter artifact name %q", name)
	}
	return nil
}

func validateOutputCollisions(files []output.File) error {
	seen := make(map[string]string, len(files))
	for _, file := range files {
		absolute, err := filepath.Abs(filepath.Clean(file.Path))
		if err != nil {
			return fmt.Errorf("resolve output path %q: %w", file.Path, err)
		}
		key := absolute
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("output path collision: %q and %q", previous, file.Path)
		}
		seen[key] = file.Path
	}
	return nil
}

func writeBytes(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func usagef(format string, arguments ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, arguments...)}
}
