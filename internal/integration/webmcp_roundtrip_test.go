package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/wheelsmif/geovisor/internal/compiler"
	"github.com/wheelsmif/geovisor/internal/emitter"
	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

// The round-trip harness closes the loop the review found missing (GV-036). The
// extractor and the generated WebMCP runtime are two implementations of role
// resolution, accessible-name computation, and element addressing; until now no
// test ran one against the other, which is why GV-001, GV-003, and GV-004 were
// invisible. Here the emitted module is imported and executed against the same
// DOM the observation came from.
const roundTripDriver = "client/test/webmcp-roundtrip.mjs"

// Tool IDs are a slug plus a twelve-character content digest. Tests address
// tools by slug so they do not churn when unrelated content changes the digest.
var toolIDPattern = regexp.MustCompile(`^(.+)-[0-9a-f]{12}$`)

type roundTripReport struct {
	ModuleError string                 `json:"moduleError"`
	Elements    []roundTripElement     `json:"elements"`
	Tools       []roundTripToolOutcome `json:"tools"`
}

type roundTripElement struct {
	Index         int    `json:"index"`
	Tag           string `json:"tag"`
	ID            string `json:"id"`
	AriaLabel     string `json:"ariaLabel"`
	Name          string `json:"name"`
	Value         string `json:"value"`
	Checked       bool   `json:"checked"`
	SelectedIndex *int   `json:"selectedIndex"`
	SelectedLabel string `json:"selectedLabel"`
	Text          string `json:"text"`
}

type roundTripToolOutcome struct {
	Name       string             `json:"name"`
	Input      map[string]any     `json:"input"`
	Error      string             `json:"error"`
	Changed    []int              `json:"changed"`
	Clicked    []int              `json:"clicked"`
	Dispatched []int              `json:"dispatched"`
	State      []roundTripElement `json:"state"`
}

// resolved reports the document-order indexes the tool's actions addressed. The
// runtime dispatches click for click actions and input/change for the rest, so
// the dispatch targets identify the resolved elements even when applying the
// value changed nothing.
func (outcome roundTripToolOutcome) resolved() []int {
	seen := make(map[int]struct{})
	indexes := make([]int, 0, len(outcome.Clicked)+len(outcome.Dispatched)+len(outcome.Changed))
	for _, group := range [][]int{outcome.Clicked, outcome.Dispatched, outcome.Changed} {
		for _, index := range group {
			if _, exists := seen[index]; exists {
				continue
			}
			seen[index] = struct{}{}
			indexes = append(indexes, index)
		}
	}
	sort.Ints(indexes)
	return indexes
}

func (report roundTripReport) bySlug(t *testing.T, slug string) roundTripToolOutcome {
	t.Helper()
	matches := make([]roundTripToolOutcome, 0, 1)
	for _, tool := range report.Tools {
		if toolSlug(tool.Name) == slug {
			matches = append(matches, tool)
		}
	}
	if len(matches) != 1 {
		t.Fatalf(
			"want exactly one tool with slug %q, found %d; registered: %s",
			slug, len(matches), strings.Join(report.toolNames(), ", "),
		)
	}
	return matches[0]
}

func (report roundTripReport) toolNames() []string {
	names := make([]string, 0, len(report.Tools))
	for _, tool := range report.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func toolSlug(id string) string {
	if match := toolIDPattern.FindStringSubmatch(id); match != nil {
		return match[1]
	}
	return id
}

// knownFailure marks a harness test that asserts correct behavior the pipeline
// does not yet produce. The assertions are the specification; the skip records
// that the finding is open. Phase 2 of
// docs/reviews/2026-09-10-remediation-plan.md closes these by deleting the
// call, not by weakening the assertion. Grep for "known failure" to list them.
func knownFailure(t *testing.T, finding, summary string) {
	t.Helper()
	t.Skipf("known failure %s: %s", finding, summary)
}

func TestWebMCPRuntimeResolvesAgainstOriginatingDOM(t *testing.T) {
	_, report := runRoundTrip(t, fixturePath, nil)

	if report.ModuleError != "" {
		t.Fatalf("emitted WebMCP module failed to load: %s", report.ModuleError)
	}
	if len(report.Tools) == 0 {
		t.Fatal("emitted WebMCP module registered no tools")
	}

	// Every advertised tool must work against the DOM it was derived from. A
	// tool that cannot resolve or apply is a tool a model will call and fail.
	for _, tool := range report.Tools {
		if tool.Error != "" {
			t.Errorf("tool %q failed to execute: %s", tool.Name, tool.Error)
			continue
		}
		if len(tool.resolved()) == 0 {
			t.Errorf("tool %q executed without resolving to any element", tool.Name)
		}
	}
}

func TestWebMCPRuntimeSelectAppliesAdvertisedOption(t *testing.T) {
	// GV-001: the extractor advertises option labels while the runtime assigns
	// the advertised string to element.value, so every <select> whose option
	// values differ from their visible text yields a permanently broken tool.
	//
	// Note for the fix: forms.html option values are sensitive sentinels that
	// pipeline tests assert never reach an artifact, so the repair cannot be
	// "emit the option value". The runtime has to select by the same label it
	// advertises.
	_, report := runRoundTrip(t, fixturePath, nil)

	tool := report.bySlug(t, "plan")
	if tool.Error != "" {
		t.Fatalf("select tool %q failed to execute: %s", tool.Name, tool.Error)
	}
	requested, ok := tool.Input["plan"].(string)
	if !ok {
		t.Fatalf("select tool %q was not given a plan value: %#v", tool.Name, tool.Input)
	}
	for _, state := range tool.State {
		if state.Tag != "select" {
			continue
		}
		if state.SelectedIndex == nil || *state.SelectedIndex < 0 {
			t.Fatalf("select %q has no selected option after execution", tool.Name)
		}
		// The selection must land on the option carrying the advertised label.
		// Asserting the label, not the value, is the point of GV-001: option
		// values never leave the page, so the label is the only shared key.
		if state.SelectedLabel != requested {
			t.Fatalf(
				"select %q requested %q but selected option is labelled %q",
				tool.Name, requested, state.SelectedLabel,
			)
		}
		return
	}
	t.Fatalf("select tool %q never resolved to a select element", tool.Name)
}

func TestWebMCPRuntimeSemanticStrategyResolvesWithoutCSSFallback(t *testing.T) {
	// GV-003 and GV-004 both fail by silently degrading to the CSS fallback:
	// the semantic strategy never matches, the runtime swallows the failure,
	// and a passing tool hides a broken locator. Removing the CSS fallback
	// leaves the semantic strategy as the only strategy, which makes that
	// degradation observable without adding test hooks to generated code.
	//
	// This is also how GV-049 was found: the runtime's role table is a strict
	// subset of the extractor's, so locators scoped by details, fieldset, nav,
	// dialog, or summary can never match.
	// Sole remaining cause: disambiguation writes a display name ("Query (2)")
	// into the locator's match key, and no element carries that name. Phase 2.5
	// separates the display name from the match key.
	knownFailure(t, "GV-004", "disambiguated locator names do not exist in the DOM")

	_, report := runRoundTrip(t, fixturePath, stripCSSFallbacks)

	if report.ModuleError != "" {
		t.Fatalf("semantic-only WebMCP module failed to load: %s", report.ModuleError)
	}
	for _, tool := range report.Tools {
		if tool.Error != "" {
			t.Errorf("tool %q cannot resolve semantically: %s", tool.Name, tool.Error)
		}
	}
}

// stripCSSFallbacks removes every CSS strategy that a semantic strategy can
// replace. Candidates and path nodes without a semantic half keep their CSS, so
// the document stays valid under tir.Validate.
func stripCSSFallbacks(document *tir.Document) {
	stripNodes := func(nodes []tir.PathNode) {
		for index := range nodes {
			if nodes[index].Semantic != nil {
				nodes[index].CSSFallback = ""
			}
		}
	}
	for ti := range document.Tools {
		tool := &document.Tools[ti]
		for li := range tool.Locators {
			locator := &tool.Locators[li]
			stripNodes(locator.FramePath)
			stripNodes(locator.ShadowPath)
			if locator.Semantic != nil {
				locator.CSSFallback = ""
			}
		}
	}
}

func runRoundTrip(
	t *testing.T,
	fixture string,
	transform func(*tir.Document),
) (*tir.Document, roundTripReport) {
	t.Helper()
	root := repositoryRoot(t)
	node := requireNodeForRoundTrip(t)

	document, err := compiler.Compile(compiler.Input{
		Source: observation.Source{
			Kind:         observation.SourceLaunchURL,
			RequestedURL: "https://roundtrip.example/",
			FinalURL:     "https://roundtrip.example/",
		},
		Batches: []observation.Batch{extractForRoundTrip(t, node, root, fixture)},
	})
	if err != nil {
		t.Fatalf("compile %s: %v", fixture, err)
	}
	if transform != nil {
		transform(document)
	}

	result, err := emitter.DefaultRegistry().Emit(
		context.Background(),
		emitter.FormatWebMCP,
		document,
		emitter.Options{},
	)
	if err != nil {
		t.Fatalf("emit WebMCP for %s: %v", fixture, err)
	}
	modulePath := filepath.Join(t.TempDir(), "tools.webmcp.mjs")
	if err := os.WriteFile(modulePath, result.Primary.Data, 0o600); err != nil {
		t.Fatal(err)
	}

	output := runDriver(t, node, root, "execute", fixture, modulePath)
	var report roundTripReport
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode round-trip report: %v\n%s", err, output)
	}
	return document, report
}

func extractForRoundTrip(t *testing.T, node, root, fixture string) observation.Batch {
	t.Helper()
	var batch observation.Batch
	output := runDriver(t, node, root, "extract", fixture)
	if err := json.Unmarshal(output, &batch); err != nil {
		t.Fatalf("decode extracted batch for %s: %v", fixture, err)
	}
	return batch
}

func runDriver(t *testing.T, node, root string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command(node, append([]string{roundTripDriver}, arguments...)...)
	command.Dir = root
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("round-trip driver %v: %v\n%s", arguments, err, stderr.String())
	}
	return output
}

func requireNodeForRoundTrip(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GEOVISOR_REQUIRE_BROWSER") == "1" {
			t.Fatal("GEOVISOR_REQUIRE_BROWSER=1 but Node.js is unavailable for the WebMCP round-trip harness")
		}
		t.Skip("Node.js is required to execute the generated WebMCP runtime")
	}
	return node
}
