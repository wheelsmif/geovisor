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

// roundTripInputs are the files the Node driver reads. The Go test cache tracks
// files the test process itself opens, not files a subprocess opens, so editing
// the driver or a committed bundle would otherwise leave a cached PASS while the
// behavior under test had changed -- which it did, silently, during Phase 2.
// Reading them here declares them as inputs (GV-050).
var roundTripInputs = []string{
	roundTripDriver,
	"internal/payload/extractor.js",
	"internal/emitter/webmcp-runtime.js",
}

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

// Phase 1 introduced a knownFailure helper so these tests could assert correct
// behavior while the findings they described were still open. Every one is now
// closed, so the helper is gone; staticcheck would flag it as dead code, which
// is the outcome the phase was aiming for.

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
	//
	// The fixture's <select> is owned by a form, so since GV-007 it is reached
	// as a parameter of the form tool rather than as a standalone tool.
	_, report := runRoundTrip(t, fixturePath, nil)

	tool := report.bySlug(t, "profile-form")
	if tool.Error != "" {
		t.Fatalf("form tool %q failed to execute: %s", tool.Name, tool.Error)
	}
	requested, ok := tool.Input["plan"].(string)
	if !ok {
		t.Fatalf("form tool %q was not given a plan value: %#v", tool.Name, tool.Input)
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

// TestWebMCPRuntimeResolvesIdenticalNamesToDistinctElements is GV-004's real
// acceptance criterion. The semantic-only test above only proves each locator
// resolves to *something*; three tools all resolving to the same element would
// satisfy it. The fixture has three textboxes named "Query", two of them in one
// scope, so distinctness is what separates a working ordinal from a locator
// that always returns the first match.
// TestWebMCPRuntimeDoesNotRegisterStandaloneFormControls is GV-007. A control
// owned by a form is a parameter of that form's tool. Registering it again as
// its own tool gives an agent two ways to fill one field and no basis for
// choosing. Submit buttons stay standalone: they are actions, not parameters.
func TestWebMCPRuntimeDoesNotRegisterStandaloneFormControls(t *testing.T) {
	_, report := runRoundTrip(t, fixturePath, nil)

	form := report.bySlug(t, "profile-form")
	if toolSlug(form.Name) != "profile-form" {
		t.Fatalf("expected the composite form tool, got %q", form.Name)
	}
	claimed := []string{"display-name", "nickname", "plan", "email-alerts", "password"}
	for _, slug := range claimed {
		for _, tool := range report.Tools {
			if toolSlug(tool.Name) == slug {
				t.Errorf("form-owned control %q was also registered as a standalone tool", slug)
			}
		}
	}
	// The submit button is an action, not a parameter, and remains callable
	// without filling the form.
	_ = report.bySlug(t, "save-profile")
	// Controls that are not owned by a form still get their own tools.
	if got := countSlugsWithPrefix(report, "query"); got != 3 {
		t.Errorf("want 3 standalone query tools outside the form, got %d", got)
	}
}

func countSlugsWithPrefix(report roundTripReport, prefix string) int {
	count := 0
	for _, tool := range report.Tools {
		if strings.HasPrefix(toolSlug(tool.Name), prefix) {
			count++
		}
	}
	return count
}

func TestWebMCPRuntimeResolvesIdenticalNamesToDistinctElements(t *testing.T) {
	_, report := runRoundTrip(t, fixturePath, stripCSSFallbacks)

	type resolvedTool struct {
		name  string
		index int
	}
	queries := make([]resolvedTool, 0, 3)
	for _, tool := range report.Tools {
		if !strings.HasPrefix(toolSlug(tool.Name), "query") {
			continue
		}
		if tool.Error != "" {
			t.Fatalf("query tool %q failed to execute: %s", tool.Name, tool.Error)
		}
		indexes := tool.resolved()
		if len(indexes) != 1 {
			t.Fatalf("query tool %q resolved to %d elements, want exactly 1", tool.Name, len(indexes))
		}
		queries = append(queries, resolvedTool{name: tool.Name, index: indexes[0]})
	}
	if len(queries) != 3 {
		t.Fatalf(
			"want three tools named Query, found %d; registered: %s",
			len(queries), strings.Join(report.toolNames(), ", "),
		)
	}
	for i := range queries {
		for j := i + 1; j < len(queries); j++ {
			if queries[i].index == queries[j].index {
				t.Fatalf(
					"tools %q and %q both resolved to element %d; identical names are not being disambiguated",
					queries[i].name, queries[j].name, queries[i].index,
				)
			}
		}
	}
}

// TestWebMCPRuntimeResolvesFramePathsToTheCorrectFrame covers GV-003.
//
// The fixture is the case both halves of the old frame locator got wrong: two
// wrappers each holding one identical, unnamed frame. A selector counting
// element siblings gives both frames position 1, and a name comparison has
// nothing to compare, so resolution returned the first frame in the document
// for both tools -- silently, because the wrong element resolved successfully.
//
// The two tools differ *only* in their frame path, and their locators carry the
// same role and name, so reaching the right input proves the frame path did the
// work.
func TestWebMCPRuntimeResolvesFramePathsToTheCorrectFrame(t *testing.T) {
	const fixture = "testdata/corpus/frames.html"

	_, report := runRoundTripBatches(
		t,
		fixture,
		nil,
		frameBatch(0, "Query one"),
		frameBatch(1, "Query two"),
	)

	if report.ModuleError != "" {
		t.Fatalf("emitted WebMCP module failed to load: %s", report.ModuleError)
	}
	first := report.bySlug(t, "query-one")
	second := report.bySlug(t, "query-two")
	for _, tool := range []roundTripToolOutcome{first, second} {
		if tool.Error != "" {
			t.Fatalf("tool %q failed to execute: %s", tool.Name, tool.Error)
		}
		if got := len(tool.resolved()); got != 1 {
			t.Fatalf("tool %q resolved to %d elements, want exactly 1", tool.Name, got)
		}
	}
	if first.resolved()[0] == second.resolved()[0] {
		t.Fatalf(
			"tools %q and %q both resolved to element %d; the frame path is not selecting the frame",
			first.Name, second.Name, first.resolved()[0],
		)
	}
	// Element indexes are assigned in document order with each frame's contents
	// following its frame element, so the frame with the lower index holds the
	// earlier element. Frame 0's tool must reach the earlier input.
	if first.resolved()[0] > second.resolved()[0] {
		t.Errorf(
			"tool %q reached element %d and %q reached element %d; frame order is inverted",
			first.Name, first.resolved()[0], second.Name, second.resolved()[0],
		)
	}
}

// frameBatch builds the observation a browser source would report for a control
// inside the frame at `index` of the top document.
//
// The frame path node shape is the contract asserted by
// TestFrameTraversalNodesAddressFramesByIndexAlone in internal/browser: role
// only, addressed by ordinal, with no name and no CSS fallback.
func frameBatch(index int, name string) observation.Batch {
	order := 0
	return observation.Batch{
		CoverageReported: true,
		Frames:           []observation.Frame{},
		Interactions: []observation.Interaction{
			{
				Kind:      observation.InteractionControl,
				FramePath: []observation.FrameReference{{Index: index}},
				Scope:     []observation.SemanticNode{},
				Role:      "textbox",
				Name:      name,
				Parameters: []observation.Parameter{
					{Name: "query", Type: observation.ValueString, SourceOrder: &order},
				},
				Locators: []observation.Locator{
					{
						FramePath: []observation.PathNode{
							{Semantic: &observation.SemanticNode{Role: "iframe", Nth: index}},
						},
						Semantic: &observation.SemanticLocator{Role: "textbox", Name: "Query"},
						Evidence: []observation.Evidence{
							{Kind: observation.EvidenceAccessibility, Reference: "semantic:textbox", Score: 0.9},
						},
					},
				},
				Actions: []observation.Action{
					{
						Kind:           observation.ActionFill,
						InputParameter: "query",
						LocatorIndexes: []int{0},
						SideEffect: observation.SideEffect{
							Class:     observation.SideEffectUnknown,
							Rationale: "Changing a control may invoke page handlers.",
						},
					},
				},
				Evidence: []observation.Evidence{
					{Kind: observation.EvidenceAccessibility, Reference: "name:aria-label", Score: 0.96},
				},
			},
		},
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
	declareRoundTripInputs(t, root, fixture)
	batch := extractForRoundTrip(t, node, root, fixture)
	return runRoundTripBatches(t, fixture, transform, batch)
}

// runRoundTripBatches compiles supplied observation batches, emits WebMCP, and
// executes the module against the fixture. Tests that need observations the
// frame-local extractor cannot produce on its own -- notably multi-frame
// coverage, which a browser source assembles -- build the batches themselves.
func runRoundTripBatches(
	t *testing.T,
	fixture string,
	transform func(*tir.Document),
	batches ...observation.Batch,
) (*tir.Document, roundTripReport) {
	t.Helper()
	root := repositoryRoot(t)
	node := requireNodeForRoundTrip(t)
	declareRoundTripInputs(t, root, fixture)

	document, err := compiler.Compile(compiler.Input{
		Source: observation.Source{
			Kind:         observation.SourceLaunchURL,
			RequestedURL: "https://roundtrip.example/",
			FinalURL:     "https://roundtrip.example/",
		},
		Batches: batches,
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

func declareRoundTripInputs(t *testing.T, root, fixture string) {
	t.Helper()
	for _, name := range append([]string{fixture}, roundTripInputs...) {
		if _, err := os.ReadFile(filepath.Join(root, name)); err != nil {
			t.Fatalf("round-trip input %s: %v", name, err)
		}
	}
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
