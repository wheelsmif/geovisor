package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/proto"
)

const (
	maxEndpointResponse    = 1 << 20
	endpointRequestTimeout = 10 * time.Second
)

var errEndpointRedirect = errors.New("CDP endpoint redirected")

func newEndpointClient() *http.Client {
	return &http.Client{
		Timeout:   endpointRequestTimeout,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.Response != nil && request.Response.Body != nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(request.Response.Body, maxEndpointResponse))
				_ = request.Response.Body.Close()
			}
			return errEndpointRedirect
		},
	}
}

// cdpBrowser is the proto client for one CDP socket. It does not use the rod
// root package: that package's Browser.Connect reaches rod/lib/launcher.
type cdpBrowser struct {
	ctx    context.Context
	client *cdp.Client
}

func (browser *cdpBrowser) Call(ctx context.Context, sessionID, method string, params interface{}) ([]byte, error) {
	return browser.client.Call(ctx, sessionID, method, params)
}

func (browser *cdpBrowser) GetContext() context.Context {
	if browser == nil || browser.ctx == nil {
		return context.Background()
	}
	return browser.ctx
}

func (browser *cdpBrowser) Context(ctx context.Context) *cdpBrowser {
	clone := *browser
	clone.ctx = ctx
	return &clone
}

type browserConnection struct {
	browser *cdpBrowser
	socket  *cdp.WebSocket
}

func connectBrowser(ctx context.Context, controlURL string) (*browserConnection, error) {
	socket := &cdp.WebSocket{}
	if err := socket.Connect(ctx, controlURL, nil); err != nil {
		return nil, err
	}
	client := cdp.New().Start(socket)
	// CDP events must be consumed or the socket reader blocks.
	go func() {
		for range client.Event() {
		}
	}()
	instance := &cdpBrowser{ctx: ctx, client: client}
	if err := (proto.TargetSetDiscoverTargets{Discover: true}).Call(instance); err != nil {
		_ = socket.Close()
		return nil, err
	}
	return &browserConnection{browser: instance, socket: socket}, nil
}

func (connection *browserConnection) closeTransport() {
	if connection != nil && connection.socket != nil {
		_ = connection.socket.Close()
	}
}

func resolveControlURL(ctx context.Context, endpoint string) (string, error) {
	parsed, err := validateEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "ws" || parsed.Scheme == "wss" {
		return parsed.String(), nil
	}

	versionURL := *parsed
	if !strings.HasSuffix(strings.TrimRight(versionURL.Path, "/"), "/json/version") {
		versionURL.Path = strings.TrimRight(versionURL.Path, "/") + "/json/version"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := newEndpointClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxEndpointResponse))
		return "", fmt.Errorf("CDP endpoint returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxEndpointResponse))
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := decoder.Decode(&version); err != nil {
		return "", fmt.Errorf("decode CDP version response: %w", err)
	}
	if version.WebSocketDebuggerURL == "" {
		return "", errors.New("CDP version response has no webSocketDebuggerUrl")
	}
	resolved, err := url.Parse(version.WebSocketDebuggerURL)
	if err != nil || resolved.Host == "" || resolved.Scheme != "ws" && resolved.Scheme != "wss" {
		return "", errors.New("CDP version response contains an invalid WebSocket URL")
	}
	pinned, err := pinDebuggerURL(parsed, resolved)
	if err != nil {
		return "", err
	}
	return pinned.String(), nil
}

func pinDebuggerURL(endpoint, advertised *url.URL) (*url.URL, error) {
	if advertised == nil || advertised.Host == "" || advertised.Scheme != "ws" && advertised.Scheme != "wss" {
		return nil, errors.New("CDP version response contains an invalid WebSocket URL")
	}
	pinned := *advertised
	pinned.User = nil
	pinned.Host = endpoint.Host
	switch endpoint.Scheme {
	case "https", "wss":
		pinned.Scheme = "wss"
	default:
		pinned.Scheme = "ws"
	}
	return &pinned, nil
}
