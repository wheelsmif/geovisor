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

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
)

const maxEndpointResponse = 1 << 20

type browserConnection struct {
	browser *rod.Browser
	socket  *cdp.WebSocket
}

func connectBrowser(ctx context.Context, controlURL string) (*browserConnection, error) {
	socket := &cdp.WebSocket{}
	if err := socket.Connect(ctx, controlURL, nil); err != nil {
		return nil, err
	}
	client := cdp.New().Start(socket)
	instance := rod.New().Context(ctx).Client(client).NoDefaultDevice()
	if err := instance.Connect(); err != nil {
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
	response, err := http.DefaultClient.Do(request)
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
	return resolved.String(), nil
}
