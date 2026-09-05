package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-rod/rod/lib/launcher"
)

func TestLaunchSmoke(t *testing.T) {
	executable, found := launcher.LookPath()
	if !found {
		if os.Getenv("GEOVISOR_REQUIRE_BROWSER") == "1" {
			t.Fatal("GEOVISOR_REQUIRE_BROWSER=1 but no Chromium executable was found")
		}
		t.Skip("no Chromium executable found; set GEOVISOR_REQUIRE_BROWSER=1 to require browser tests")
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(
			writer,
			`<!doctype html><button aria-label="CLI smoke action">Run</button>`,
		)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := RunContext(context.Background(), []string{
		"inspect", server.URL,
		"--browser-executable", executable,
		"--timeout", "45s",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("inspect launch: %v; diagnostics: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"tools":[`) ||
		!strings.Contains(stdout.String(), "CLI smoke action") {
		t.Fatalf("unexpected TIR output: %s", stdout.String())
	}
}
