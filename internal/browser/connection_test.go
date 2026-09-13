package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	want := "ws://" + parsed.Host + "/devtools/browser/id"
	if got != want {
		t.Fatalf("control URL = %q, want pinned %q", got, want)
	}
}

func TestResolveControlURLPinsForeignLoopbackAdvertisement(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(writer).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/foreign",
		}); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(server.Close)

	got, err := resolveControlURL(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("resolveControlURL: %v", err)
	}
	if strings.Contains(got, ":9222") {
		t.Fatalf("advertised loopback port was not pinned: %s", got)
	}
	if !strings.HasPrefix(got, "ws://") {
		t.Fatalf("scheme = %q", got)
	}
}

func TestEndpointClientDisablesEnvironmentProxy(t *testing.T) {
	t.Parallel()
	client := newEndpointClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("CDP HTTP client must not use the environment proxy")
	}
}

func TestEndpointClientClosesRedirectBody(t *testing.T) {
	t.Parallel()
	tracker := &closeTracker{Reader: strings.NewReader("redirect body")}
	request, err := http.NewRequest(http.MethodGet, "http://example.test/next", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Response = &http.Response{Body: tracker}
	if err := newEndpointClient().CheckRedirect(request, nil); !errors.Is(err, errEndpointRedirect) {
		t.Fatalf("CheckRedirect = %v", err)
	}
	if !tracker.closed {
		t.Fatal("redirect response body was not closed")
	}
}

type closeTracker struct {
	io.Reader
	closed bool
}

func (tracker *closeTracker) Close() error {
	tracker.closed = true
	return nil
}
