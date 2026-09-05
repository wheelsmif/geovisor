package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wheelsmif/geovisor/internal/compiler"
	"github.com/wheelsmif/geovisor/internal/emitter"
	"github.com/wheelsmif/geovisor/internal/observation"
	"github.com/wheelsmif/geovisor/internal/tir"
)

const fixturePath = "testdata/corpus/forms.html"

var sensitiveSentinels = []string{
	"CURRENT-DISPLAY-NAME-SECRET",
	"CURRENT-PASSWORD-SECRET",
	"CURRENT-QUERY-SECRET",
	"HIDDEN-BASIC-ID",
	"HIDDEN-PRO-ID",
	"CURRENT-URL-SECRET",
	"CURRENT-FRAGMENT-SECRET",
}

func TestPayloadCompilerEmitterPipeline(t *testing.T) {
	root := repositoryRoot(t)
	batch := extractFixture(t, root)
	assertNoSensitiveValues(t, mustJSON(t, batch))

	input := compiler.Input{
		Source: observation.Source{
			Kind:         observation.SourceLaunchURL,
			RequestedURL: "https://corpus.example/forms",
			FinalURL:     "https://corpus.example/forms",
		},
		Batches: []observation.Batch{batch},
	}
	baseline := compileAndEmit(t, root, input)

	reversed := cloneInput(t, input)
	reverse(reversed.Batches[0].Interactions)
	for index := range reversed.Batches[0].Interactions {
		reverse(reversed.Batches[0].Interactions[index].Evidence)
	}
	shuffled := compileAndEmit(t, root, reversed)
	if !reflect.DeepEqual(baseline, shuffled) {
		t.Fatal("payload pipeline changed bytes after unordered input reversal")
	}

	repeatedBatch := extractFixture(t, root)
	if !bytes.Equal(mustJSON(t, batch), mustJSON(t, repeatedBatch)) {
		t.Fatal("repeated browser payload extraction changed bytes")
	}
}

func TestInaccessibleFrameCorpusCompilesAsPartialCoverage(t *testing.T) {
	var input compiler.Input
	decodeFile(t, filepath.Join(repositoryRoot(t), "testdata", "corpus", "inaccessible-observation.json"), &input)
	document, err := compiler.Compile(input)
	if err != nil {
		t.Fatalf("compile inaccessible-frame fixture: %v", err)
	}
	if document.FrameCoverage.Status != tir.CoveragePartial ||
		len(document.FrameCoverage.Uncovered) != 1 {
		t.Fatalf("coverage = %+v, want one explicit uncovered frame", document.FrameCoverage)
	}
}

func BenchmarkCorpusCompileAndEmit(b *testing.B) {
	root := repositoryRoot(b)
	var seed observation.Batch
	decodeFile(b, filepath.Join(root, "internal", "payload", "testdata", "observation-batch.json"), &seed)
	input := benchmarkInput(seed, 100)
	registry := emitter.DefaultRegistry()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		document, err := compiler.Compile(input)
		if err != nil {
			b.Fatal(err)
		}
		for _, format := range registry.Formats() {
			if _, err := registry.Emit(ctx, format, document, emitter.Options{Strict: true}); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func compileAndEmit(t *testing.T, root string, input compiler.Input) map[emitter.Format]emitter.Result {
	t.Helper()
	document, err := compiler.Compile(input)
	if err != nil {
		t.Fatalf("compile observation: %v", err)
	}
	registry := emitter.DefaultRegistry()
	results := make(map[emitter.Format]emitter.Result)
	for _, format := range registry.Formats() {
		result, err := registry.Emit(context.Background(), format, document, emitter.Options{Strict: true})
		if err != nil {
			t.Fatalf("emit %s: %v", format, err)
		}
		assertNoSensitiveValues(t, result.Primary.Data)
		switch format {
		case emitter.FormatTIRJSON:
			validateSchema(t, root, "tir.schema.json", result.Primary.Data)
		case emitter.FormatMCP:
			validateSchema(t, root, "mcp-tools-list-2026-07-28.schema.json", result.Primary.Data)
		case emitter.FormatOpenAI:
			validateSchema(t, root, "openai-function-tools.schema.json", result.Primary.Data)
		case emitter.FormatWebMCP:
			checkJavaScriptSyntax(t, result.Primary.Data)
		}
		if result.Companion != nil {
			assertNoSensitiveValues(t, result.Companion.Data)
			validateSchema(t, root, "emitter-bindings.schema.json", result.Companion.Data)
		}
		results[format] = result
	}
	return results
}

func validateSchema(t *testing.T, root, schemaName string, payload []byte) {
	t.Helper()
	schemaDirectory := filepath.Join(root, "schemas")
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{
		"tir.schema.json",
		"mcp-tools-list-2026-07-28.schema.json",
		"openai-function-tools.schema.json",
		"emitter-bindings.schema.json",
	} {
		var resource any
		decodeFile(t, filepath.Join(schemaDirectory, name), &resource)
		id := resource.(map[string]any)["$id"].(string)
		if err := compiler.AddResource(id, resource); err != nil {
			t.Fatalf("add schema resource %s: %v", name, err)
		}
	}
	schemaID := fmt.Sprintf("https://wheelsmif.github.io/geovisor/schemas/%s", schemaName)
	schema, err := compiler.Compile(schemaID)
	if err != nil {
		t.Fatalf("compile schema %s: %v", schemaName, err)
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode payload for %s: %v", schemaName, err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatalf("validate payload against %s: %v", schemaName, err)
	}
}

func checkJavaScriptSyntax(t *testing.T, source []byte) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to syntax-check generated WebMCP JavaScript")
	}
	path := filepath.Join(t.TempDir(), "tools.webmcp.mjs")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, "--check", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated WebMCP JavaScript is invalid: %v\n%s", err, output)
	}
}

func extractFixture(t *testing.T, root string) observation.Batch {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for payload pipeline integration")
	}
	command := exec.Command(node, "client/test/extract-fixture.mjs", fixturePath)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("extract fixture: %v", err)
	}
	var batch observation.Batch
	if err := json.Unmarshal(output, &batch); err != nil {
		t.Fatalf("decode extracted fixture: %v", err)
	}
	return batch
}

func benchmarkInput(seed observation.Batch, count int) compiler.Input {
	interactions := make([]observation.Interaction, 0, count)
	for index := 0; index < count; index++ {
		item := seed.Interactions[0]
		item.Name = fmt.Sprintf("Benchmark control %03d", index)
		item.Scope = []observation.SemanticNode{{Role: "region", Name: fmt.Sprintf("Region %03d", index)}}
		interactions = append(interactions, item)
	}
	return compiler.Input{
		Source:  observation.Source{Kind: observation.SourceLaunchURL},
		Batches: []observation.Batch{{Interactions: interactions}},
	}
}

func repositoryRoot(tb testing.TB) string {
	tb.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		tb.Fatal(err)
	}
	return root
}

func decodeFile(tb testing.TB, path string, destination any) {
	tb.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		tb.Fatalf("decode %s: %v", path, err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertNoSensitiveValues(t *testing.T, data []byte) {
	t.Helper()
	for _, sentinel := range sensitiveSentinels {
		if strings.Contains(string(data), sentinel) {
			t.Fatalf("artifact leaked sensitive sentinel %q", sentinel)
		}
	}
}

func cloneInput(t *testing.T, input compiler.Input) compiler.Input {
	t.Helper()
	data := mustJSON(t, input)
	var cloned compiler.Input
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func reverse[T any](values []T) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
