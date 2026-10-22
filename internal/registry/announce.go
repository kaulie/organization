// Package registry reports this process's listen address to service-registry.
//
// The call is one PUT .../announce after Listen succeeds and before the
// server accepts requests. It is not a heartbeat and it is not a declarative
// instance sync: other addresses of this service are left alone. See
// service-registry docs/register.md.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	// AnnounceTimeout bounds the startup registration call.
	AnnounceTimeout = 5 * time.Second
	// DeregisterTimeout bounds the delete issued while the process is exiting.
	// stop.sh waits 15s after SIGTERM before SIGKILL; this stays well inside
	// that budget so the HTTP server can still drain.
	DeregisterTimeout = 2 * time.Second

	defaultRegistryURL = "http://127.0.0.1:4240"
	defaultNamespace   = "default"
	defaultService     = "organization"
)

// Client talks to one service-registry namespace and service.
type Client struct {
	BaseURL   string
	Namespace string
	Service   string
	Token     string
	HTTP      *http.Client
}

// Announcement is the address the registry stored for this process.
type Announcement struct {
	ID      string
	Created bool
	Host    string
	Port    int
}

// FromEnv reads the same names the contract script uses.
// REGISTRY_URL defaults to the loopback registry. SERVICE_PORT is this
// process's own listen port and is never used to find the registry.
func FromEnv() Client {
	return Client{
		BaseURL:   envOr("REGISTRY_URL", defaultRegistryURL),
		Namespace: envOr("REGISTRY_NS", defaultNamespace),
		Service:   envOr("SERVICE_NAME", defaultService),
		Token:     strings.TrimSpace(os.Getenv("REGISTRY_TOKEN")),
	}
}

// Announce registers this process's listen address.
// host is omitted when empty so the registry records the TCP peer.
// A non-2xx response, including 404 when the contract is missing, is an error:
// the caller must abort startup rather than serve with no catalog entry.
func (c Client) Announce(ctx context.Context, port int, host string) (Announcement, error) {
	if port < 1 || port > 65535 {
		return Announcement{}, fmt.Errorf("listen port %d is outside 1-65535", port)
	}
	osName, err := normalizeOS(currentGOOS())
	if err != nil {
		return Announcement{}, err
	}
	body := announceBody{
		Port: port,
		OS:   osName,
		Host: sanitizeHost(host),
		Arch: normalizeArch(currentGOARCH()),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Announcement{}, err
	}
	endpoint, err := c.serviceURL()
	if err != nil {
		return Announcement{}, err
	}
	status, respBody, err := c.do(ctx, http.MethodPut, endpoint+"/announce", raw)
	if err != nil {
		return Announcement{}, err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return Announcement{}, fmt.Errorf("announce %s/%s: %w", c.Namespace, c.Service, registryStatusError(status, respBody))
	}
	var parsed announceResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Announcement{}, fmt.Errorf("announce %s/%s: decode response: %w", c.Namespace, c.Service, err)
	}
	id := strings.TrimSpace(parsed.Instance.ID)
	if id == "" {
		return Announcement{}, fmt.Errorf("announce %s/%s: response has no instance.id", c.Namespace, c.Service)
	}
	gotHost := parsed.Instance.Host
	if gotHost == "" {
		gotHost = body.Host
	}
	gotPort := parsed.Instance.Port
	if gotPort == 0 {
		gotPort = port
	}
	return Announcement{ID: id, Created: parsed.Created, Host: gotHost, Port: gotPort}, nil
}

// Deregister removes the address this process announced.
// An empty id is a no-op. 404 is success: the address is already gone.
func (c Client) Deregister(ctx context.Context, instanceID string) error {
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return nil
	}
	endpoint, err := c.serviceURL()
	if err != nil {
		return err
	}
	status, respBody, err := c.do(ctx, http.MethodDelete, endpoint+"/instances/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if status == http.StatusNoContent || status == http.StatusOK || status == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("deregister instance %s: %w", id, registryStatusError(status, respBody))
}

// HostForAnnounce is the host field for a bound address.
// An unspecified address (nil, 0.0.0.0, ::) returns "" so the registry uses
// the TCP peer of this call. IPv6 is written as [addr], which is what the
// registry accepts.
func HostForAnnounce(ip net.IP) string {
	if ip == nil || ip.IsUnspecified() {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return "[" + ip.String() + "]"
}

type announceBody struct {
	Port int    `json:"port"`
	OS   string `json:"os"`
	Host string `json:"host,omitempty"`
	Arch string `json:"arch,omitempty"`
}

type announceResponse struct {
	Instance struct {
		ID   string `json:"id"`
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"instance"`
	Created bool `json:"created"`
}

func (c Client) serviceURL() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("REGISTRY_URL is empty")
	}
	ns := strings.TrimSpace(c.Namespace)
	svc := strings.TrimSpace(c.Service)
	if ns == "" || svc == "" {
		return "", fmt.Errorf("REGISTRY_NS and SERVICE_NAME are required")
	}
	return base + "/v1/namespaces/" + url.PathEscape(ns) + "/services/" + url.PathEscape(svc), nil
}

func (c Client) do(ctx context.Context, method, endpoint string, payload []byte) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(c.Token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("registry %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("registry %s: read response: %w", endpoint, err)
	}
	return resp.StatusCode, respBody, nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func registryStatusError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var parsed struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
		if parsed.Error.Code != "" {
			msg = parsed.Error.Code + ": " + parsed.Error.Message
		}
	}
	if len(msg) > 512 {
		msg = msg[:512]
	}
	return fmt.Errorf("registry returned %d: %s", status, msg)
}

func sanitizeHost(host string) string {
	h := strings.TrimSpace(host)
	switch strings.ToLower(strings.Trim(h, "[]")) {
	case "", "0.0.0.0", "::":
		return ""
	}
	if strings.Contains(h, ":") && !strings.HasPrefix(h, "[") {
		return "[" + h + "]"
	}
	return h
}

func normalizeOS(goosName string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(goosName)) {
	case "darwin":
		return "mac", nil
	case "linux", "mac", "windows":
		return strings.ToLower(strings.TrimSpace(goosName)), nil
	default:
		return "", fmt.Errorf("registry rejected os %q (allowed: linux, mac, windows)", goosName)
	}
}

func normalizeArch(goarchName string) string {
	switch strings.ToLower(strings.TrimSpace(goarchName)) {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "386", "i386", "i686":
		return "386"
	case "arm", "armv7l", "armv6l":
		return "arm"
	default:
		return ""
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// currentGOOS and currentGOARCH are variables so tests can cover the mapping
// without depending on the machine running the test.
var (
	currentGOOS   = func() string { return runtime.GOOS }
	currentGOARCH = func() string { return runtime.GOARCH }
)
