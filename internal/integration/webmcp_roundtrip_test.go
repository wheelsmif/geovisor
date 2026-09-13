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

type roundTripSubmit struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

type roundTripToolOutcome struct {
	Name        string             `json:"name"`
	Input       map[string]any     `json:"input"`
	Error       string             `json:"error"`
	Changed     []int              `json:"changed"`
	Clicked     []int              `json:"clicked"`
	Submitted   []roundTripSubmit  `json:"submitted"`
	Dispatched  []int              `json:"dispatched"`
	InputEvents []string           `json:"inputEvents"`
	State       []roundTripElement `json:"state"`
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
	seen := make(map[int]string)
	for _, tool := range report.Tools {
		if tool.Error != "" {
			t.Errorf("tool %q failed to execute: %s", tool.Name, tool.Error)
			continue
		}
		indexes := tool.resolved()
		if len(indexes) == 0 {
			t.Errorf("tool %q executed without resolving to any element", tool.Name)
			continue
		}
		for _, index := range indexes {
			if previous, exists := seen[index]; exists && previous != tool.Name {
				// Distinct tools may share a submit button (form + standalone).
				if toolSlug(tool.Name) == "save-profile" || toolSlug(previous) == "save-profile" {
					continue
				}
				t.Errorf("tools %q and %q both resolved to element %d", previous, tool.Name, index)
			}
			seen[index] = tool.Name
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
			continue
		}
		if len(tool.resolved()) == 0 {
			t.Errorf("tool %q resolved semantically to no element", tool.Name)
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
	claimed := []string{"display-name", "nickname", "plan", "email-alerts", "password", "bio"}
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
	document, report := runRoundTrip(t, "testdata/corpus/frames.html", stripCSSFallbacks)

	if report.ModuleError != "" {
		t.Fatalf("emitted WebMCP module failed to load: %s", report.ModuleError)
	}

	type framed struct {
		nth   int
		name  string
		index int
	}
	var resolved []framed
	for _, tool := range document.Tools {
		nth := framePathNth(t, tool)
		outcome := report.byName(t, tool.ID)
		if outcome.Error != "" {
			t.Fatalf("tool %q failed to execute: %s", tool.ID, outcome.Error)
		}
		indexes := outcome.resolved()
		if len(indexes) != 1 {
			t.Fatalf("tool %q resolved to %d elements, want exactly 1", tool.ID, len(indexes))
		}
		resolved = append(resolved, framed{nth: nth, name: tool.ID, index: indexes[0]})
	}
	if len(resolved) != 2 {
		t.Fatalf("want two framed tools, got %d: %s", len(resolved), strings.Join(report.toolNames(), ", "))
	}
	if resolved[0].index == resolved[1].index {
		t.Fatalf(
			"tools %q and %q both resolved to element %d; the frame path is not selecting the frame",
			resolved[0].name, resolved[1].name, resolved[0].index,
		)
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].nth < resolved[j].nth })
	if resolved[0].nth != 0 || resolved[1].nth != 1 {
		t.Fatalf("frame ordinals = %d, %d; want 0 and 1", resolved[0].nth, resolved[1].nth)
	}
	if resolved[0].index > resolved[1].index {
		t.Errorf(
			"frame 0 reached element %d and frame 1 reached element %d; frame order is inverted",
			resolved[0].index, resolved[1].index,
		)
	}
}

func TestWebMCPRuntimeResolvesAmbiguousScopesToDistinctElements(t *testing.T) {
	fixture := writeRoundTripFixture(t, `<!doctype html>
<html lang="en"><body>
  <div role="region" aria-label="Panel"><input aria-label="Query"></div>
  <div role="region" aria-label="Panel"><input aria-label="Query"></div>
</body></html>`)
	_, report := runRoundTrip(t, fixture, stripCSSFallbacks)
	var indexes []int
	for _, tool := range report.Tools {
		if !strings.HasPrefix(toolSlug(tool.Name), "query") {
			continue
		}
		if tool.Error != "" {
			t.Fatalf("query tool %q failed: %s", tool.Name, tool.Error)
		}
		got := tool.resolved()
		if len(got) != 1 {
			t.Fatalf("query tool %q resolved to %d elements", tool.Name, len(got))
		}
		indexes = append(indexes, got[0])
	}
	if len(indexes) != 2 || indexes[0] == indexes[1] {
		t.Fatalf("ambiguous scopes resolved to %v; want two distinct elements", indexes)
	}
}

func TestWebMCPFormToolDoesNotSubmit(t *testing.T) {
	_, report := runRoundTrip(t, "testdata/corpus/two-submit.html", nil)
	form := report.bySlug(t, "login")
	if form.Error != "" {
		t.Fatalf("form tool failed: %s", form.Error)
	}
	if len(form.Submitted) != 0 {
		t.Fatalf("form tool clicked submit controls: %+v", form.Submitted)
	}
	if len(form.Changed) == 0 && len(form.Dispatched) == 0 {
		t.Fatal("form tool did not fill any fields")
	}

	save := report.bySlug(t, "save")
	continueSave := report.bySlug(t, "save-and-continue")
	if save.Error != "" || continueSave.Error != "" {
		t.Fatalf("submit tools failed: %s %s", save.Error, continueSave.Error)
	}
	if len(save.Submitted) != 1 || save.Submitted[0].Name != "Save" {
		t.Fatalf("Save tool submitted %+v, want exactly Save", save.Submitted)
	}
	if len(continueSave.Submitted) != 1 || continueSave.Submitted[0].Name != "Save and continue" {
		t.Fatalf("Save and continue submitted %+v", continueSave.Submitted)
	}
}

func TestWebMCPInputSubmitIsActionNotParameter(t *testing.T) {
	_, report := runRoundTrip(t, "testdata/corpus/input-submit.html", nil)
	form := report.bySlug(t, "checkout")
	if form.Error != "" {
		t.Fatalf("checkout form failed: %s", form.Error)
	}
	if len(form.Submitted) != 0 {
		t.Fatalf("checkout form submitted %+v", form.Submitted)
	}
	for _, tool := range report.Tools {
		if toolSlug(tool.Name) == "place-order" || toolSlug(tool.Name) == "email" {
			if toolSlug(tool.Name) == "email" {
				t.Fatal("email was registered standalone")
			}
		}
	}
	place := report.bySlug(t, "place-order")
	if len(place.Submitted) != 1 {
		t.Fatalf("Place order submitted %+v, want one submit", place.Submitted)
	}
}

func TestWebMCPSiblingShadowHostsResolveDistinctly(t *testing.T) {
	document, report := runRoundTrip(t, "testdata/corpus/sibling-shadow.html", stripCSSFallbacks)
	if report.ModuleError != "" {
		t.Fatalf("module error: %s", report.ModuleError)
	}
	alpha := report.bySlug(t, "alpha")
	beta := report.bySlug(t, "beta")
	if alpha.Error != "" || beta.Error != "" {
		t.Fatalf("shadow tools failed: %s %s", alpha.Error, beta.Error)
	}
	alphaResolved := alpha.resolved()
	betaResolved := beta.resolved()
	if len(alphaResolved) != 1 || len(betaResolved) != 1 || alphaResolved[0] == betaResolved[0] {
		t.Fatalf("Alpha/Beta resolved to %v and %v", alphaResolved, betaResolved)
	}
	for _, tool := range document.Tools {
		for _, locator := range tool.Locators {
			if len(locator.ShadowPath) == 0 {
				continue
			}
			last := locator.ShadowPath[len(locator.ShadowPath)-1]
			if last.CSSFallback != "" && last.CSSFallback == "div" {
				t.Fatalf("tool %q kept a non-unique shadow host CSS %q", tool.ID, last.CSSFallback)
			}
		}
	}
}

func TestExtractCompileIDsStableWhenEarlierDuplicateNameInserted(t *testing.T) {
	baselineHTML := `<!doctype html><html lang="en"><body>
		<section role="region" aria-label="Filters"><input aria-label="Query"><input aria-label="Query"></section>
	</body></html>`
	shiftedHTML := `<!doctype html><html lang="en"><body>
		<section role="region" aria-label="Earlier"><input aria-label="Query"></section>
		<section role="region" aria-label="Filters"><input aria-label="Query"><input aria-label="Query"></section>
	</body></html>`
	baseline := compileExtracted(t, writeRoundTripFixture(t, baselineHTML))
	shifted := compileExtracted(t, writeRoundTripFixture(t, shiftedHTML))

	baselineIDs := toolIDsInScope(t, baseline, "Filters")
	shiftedIDs := toolIDsInScope(t, shifted, "Filters")
	if len(baselineIDs) != 2 || len(shiftedIDs) != 2 {
		t.Fatalf("filter tools = %v vs %v", baselineIDs, shiftedIDs)
	}
	if baselineIDs[0] != shiftedIDs[0] || baselineIDs[1] != shiftedIDs[1] {
		t.Fatalf("earlier duplicate-name control changed IDs from %v to %v", baselineIDs, shiftedIDs)
	}
}

func toolIDsInScope(t *testing.T, document *tir.Document, scope string) []string {
	t.Helper()
	var ids []string
	for _, tool := range document.Tools {
		for _, locator := range tool.Locators {
			if locator.Semantic == nil {
				continue
			}
			for _, node := range locator.Semantic.Scope {
				if node.Name == scope {
					ids = append(ids, tool.ID)
					break
				}
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func TestExtractCompileIDsStableWhenUnrelatedElementInserted(t *testing.T) {
	baselineHTML := `<!doctype html><html lang="en"><body><input><button>Go</button></body></html>`
	shiftedHTML := `<!doctype html><html lang="en"><body><div id="pad"></div><input><button>Go</button></body></html>`
	baseline := compileExtracted(t, writeRoundTripFixture(t, baselineHTML))
	shifted := compileExtracted(t, writeRoundTripFixture(t, shiftedHTML))
	baselineID := toolIDByName(t, baseline, "Go")
	shiftedID := toolIDByName(t, shifted, "Go")
	if baselineID != shiftedID {
		t.Fatalf("unrelated earlier element changed tool ID from %q to %q", baselineID, shiftedID)
	}
}

func framePathNth(t *testing.T, tool tir.Tool) int {
	t.Helper()
	for _, locator := range tool.Locators {
		if len(locator.FramePath) == 1 && locator.FramePath[0].Semantic != nil {
			return locator.FramePath[0].Semantic.Nth
		}
	}
	t.Fatalf("tool %q has no single-node frame path", tool.ID)
	return 0
}

func (report roundTripReport) byName(t *testing.T, name string) roundTripToolOutcome {
	t.Helper()
	for _, tool := range report.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered; have %s", name, strings.Join(report.toolNames(), ", "))
	return roundTripToolOutcome{}
}

func compileExtracted(t *testing.T, fixture string) *tir.Document {
	t.Helper()
	document, _ := runRoundTrip(t, fixture, nil)
	return document
}

func toolIDByName(t *testing.T, document *tir.Document, name string) string {
	t.Helper()
	for _, tool := range document.Tools {
		if tool.Name == name {
			return tool.ID
		}
	}
	t.Fatalf("tool named %q not found", name)
	return ""
}

func writeRoundTripFixture(t *testing.T, html string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.html")
	if err := os.WriteFile(path, []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
	batches := extractBatchesForRoundTrip(t, node, root, fixture)
	return runRoundTripBatches(t, fixture, transform, batches...)
}

// runRoundTripBatches compiles supplied observation batches, emits WebMCP, and
// executes the module against the fixture. The extract driver now stamps frame
// paths the same way the browser source does, so multi-frame fixtures go
// through extract → compile → execute rather than a hand-built batch.
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

	document, err := compiler.Compile(context.Background(), compiler.Input{
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

func extractBatchesForRoundTrip(t *testing.T, node, root, fixture string) []observation.Batch {
	t.Helper()
	var report struct {
		Batches []observation.Batch `json:"batches"`
	}
	output := runDriver(t, node, root, "extract", fixture)
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode extracted batches for %s: %v", fixture, err)
	}
	if len(report.Batches) == 0 {
		t.Fatalf("extract produced no batches for %s", fixture)
	}
	return report.Batches
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
	for _, name := range roundTripInputs {
		if _, err := os.ReadFile(filepath.Join(root, name)); err != nil {
			t.Fatalf("round-trip input %s: %v", name, err)
		}
	}
	path := fixture
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, fixture)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("round-trip fixture %s: %v", fixture, err)
	}
}

func requireNodeForRoundTrip(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("GEOVISOR_REQUIRE_NODE") == "1" || os.Getenv("GEOVISOR_REQUIRE_BROWSER") == "1" {
			t.Fatal("Node.js is required to execute the generated WebMCP runtime")
		}
		t.Skip("Node.js is required to execute the generated WebMCP runtime")
	}
	return node
}
