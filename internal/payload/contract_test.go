package payload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/geo-suite/geovisor/internal/observation"
)

func TestBrowserBundleInstallsStableExtractionGlobal(t *testing.T) {
	t.Parallel()

	bundle := BrowserBundle()
	if bundle == "" {
		t.Fatal("embedded browser bundle is empty")
	}
	if !strings.Contains(bundle, "__GEOVISOR_EXTRACT__") {
		t.Fatal("embedded browser bundle does not install the stable extraction global")
	}
	if strings.Contains(bundle, "sourceMappingURL") {
		t.Fatal("embedded browser bundle must not contain a source map reference")
	}
}

func TestObservationFixtureMatchesGoJSONContract(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "observation-batch.json"))
	if err != nil {
		t.Fatalf("read contract fixture: %v", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var batch observation.Batch
	if err := decoder.Decode(&batch); err != nil {
		t.Fatalf("strictly decode contract fixture: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("contract fixture has trailing JSON: %v", err)
	}

	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal observation batch: %v", err)
	}
	var fixtureValue, goValue any
	if err := json.Unmarshal(data, &fixtureValue); err != nil {
		t.Fatalf("decode fixture value: %v", err)
	}
	if err := json.Unmarshal(encoded, &goValue); err != nil {
		t.Fatalf("decode Go value: %v", err)
	}
	if !reflect.DeepEqual(goValue, fixtureValue) {
		t.Fatalf("Go observation JSON contract drifted\nfixture: %s\n     Go: %s", data, encoded)
	}

	if len(batch.Interactions) != 1 || len(batch.Interactions[0].Actions) != 1 {
		t.Fatal("contract fixture must exercise a complete interaction")
	}
}
