package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveControlURLRejectsRedirects(t *testing.T) {
	t.Parallel()
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("redirect was followed")
	}))
	t.Cleanup(destination.Close)
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, destination.URL+"/json/version", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	_, err := resolveControlURL(context.Background(), source.URL)
	if err == nil || !errors.Is(err, errEndpointRedirect) {
		t.Fatalf("error = %v, want redirect", err)
	}
}

func TestResolveControlURLReadsWebSocketURL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/json/version" {
			t.Fatalf("path = %s, want /json/version", request.URL.Path)
		}
		if err := json.NewEncoder(writer).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/id",
		}); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(server.Close)

	got, err := resolveControlURL(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("resolveControlURL: %v", err)
	}
	if got != "ws://127.0.0.1:9222/devtools/browser/id" {
		t.Fatalf("control URL = %q", got)
	}
}
