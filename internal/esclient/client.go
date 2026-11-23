// Package esclient talks to Elasticsearch and OpenSearch clusters of different
// versions and hides their API differences behind the ClusterReader interface.
package esclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultTimeout = 30 * time.Second
	// maxResponseBytes caps how much of a response is read into memory.
	maxResponseBytes = 128 << 20
)

// Config describes how to reach one cluster.
type Config struct {
	BaseURL            string
	Username           string
	Password           string
	APIKey             string // takes precedence over Username/Password
	InsecureSkipVerify bool
	Timeout            time.Duration // zero means 30 seconds
}

// Client is an HTTP client for a single cluster. It is safe for concurrent use.
type Client struct {
	cfg  Config
	base *url.URL
	http *http.Client

	mu      sync.RWMutex
	version *Version
}

// New validates cfg and returns a client. It does not contact the cluster.
func New(cfg Config) (*Client, error) {
	base, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("esclient: invalid base URL: %w", err)
	}
	if (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, errors.New("esclient: base URL must be an absolute http or https URL")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in per cluster
	}

	return &Client{
		cfg:  cfg,
		base: base,
		http: &http.Client{Transport: transport, Timeout: timeout},
	}, nil
}

// ErrResponseTooLarge is returned when a response exceeds the allowed size.
var ErrResponseTooLarge = errors.New("esclient: response is too large")

// Do sends a request and returns the raw response body. A non-nil body is
// encoded as JSON. Non-2xx responses are returned as *APIError.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("esclient: encode request body: %w", err)
		}
		payload = encoded
	}

	target := c.base.JoinPath(path)
	target.RawQuery = query.Encode()

	contentType := ""
	if body != nil {
		contentType = "application/json"
	}
	resp, err := c.roundTrip(ctx, method, target, payload, contentType, maxResponseBytes)
	if err != nil {
		return nil, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return nil, newAPIError(resp.Status, resp.Body)
	}
	return resp.Body, nil
}

// RawResponse is a cluster response that has not been interpreted. Unlike Do,
// a non-2xx status is not an error: the caller wants to see what the cluster said.
type RawResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

// roundTrip sends one request and reads at most limit bytes of the response.
func (c *Client) roundTrip(ctx context.Context, method string, target *url.URL, body []byte, contentType string, limit int64) (RawResponse, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return RawResponse{}, fmt.Errorf("esclient: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	c.authenticate(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return RawResponse{}, fmt.Errorf("esclient: %s %s: %w", method, target.Path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return RawResponse{}, fmt.Errorf("esclient: read response: %w", err)
	}
	if int64(len(data)) > limit {
		return RawResponse{}, fmt.Errorf("%w: %s %s exceeds %d bytes", ErrResponseTooLarge, method, target.Path, limit)
	}
	return RawResponse{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: data}, nil
}

// doJSON sends a request and decodes the JSON response into out.
func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	data, err := c.Do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("esclient: decode response of %s %s: %w", method, path, err)
	}
	return nil
}

func (c *Client) authenticate(req *http.Request) {
	switch {
	case c.cfg.APIKey != "":
		req.Header.Set("Authorization", "ApiKey "+c.cfg.APIKey)
	case c.cfg.Username != "":
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
}

// Version detects the server flavor and version. The result is cached after the
// first success.
func (c *Client) Version(ctx context.Context) (Version, error) {
	c.mu.RLock()
	cached := c.version
	c.mu.RUnlock()
	if cached != nil {
		return *cached, nil
	}

	var root struct {
		Version struct {
			Number       string `json:"number"`
			Distribution string `json:"distribution"`
		} `json:"version"`
	}
	query := url.Values{"filter_path": {"version.number,version.distribution"}}
	if err := c.doJSON(ctx, http.MethodGet, "", query, nil, &root); err != nil {
		return Version{}, err
	}

	flavor := FlavorElasticsearch
	if root.Version.Distribution == string(FlavorOpenSearch) {
		flavor = FlavorOpenSearch
	}
	version, err := ParseVersion(flavor, root.Version.Number)
	if err != nil {
		return Version{}, err
	}

	c.mu.Lock()
	c.version = &version
	c.mu.Unlock()
	return version, nil
}
