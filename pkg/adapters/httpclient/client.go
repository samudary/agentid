// Package httpclient provides a shared HTTP client for adapter
// implementations. It handles request construction, upstream authentication,
// and response reading — the boilerplate that every adapter would otherwise
// duplicate.
//
// Purpose-built adapters use this directly. The generic REST adapter also
// builds on top of it.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/samudary/agentid/pkg/adapters"
)

// Client wraps http.Client with upstream authentication and standard
// request/response handling for adapter implementations.
type Client struct {
	http *http.Client
	auth adapters.UpstreamAuth
}

// New creates a Client with the given upstream auth configuration.
// Accepts optional http.Client; uses http.DefaultClient behavior if nil.
func New(auth adapters.UpstreamAuth, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		http: httpClient,
		auth: auth,
	}
}

// Do builds an HTTP request, applies upstream authentication, sends it,
// and converts the response into a ToolResult. This is the primary method
// adapters should use for upstream calls.
//
// The body parameter is JSON-marshaled if non-nil. The accept parameter
// sets the Accept header (e.g., "application/vnd.github.v3+json"); if
// empty, "application/json" is used.
func (c *Client) Do(ctx context.Context, method, url string, body any, accept string) (*adapters.ToolResult, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.auth.Apply(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &adapters.ToolResult{
			Content:    []adapters.ContentBlock{{Type: "text", Text: string(respBody)}},
			IsError:    true,
			StatusCode: resp.StatusCode,
		}, nil
	}

	return &adapters.ToolResult{
		Content:    []adapters.ContentBlock{{Type: "text", Text: string(respBody)}},
		StatusCode: resp.StatusCode,
	}, nil
}

// DoRaw is like Do but returns the raw *http.Response for cases where
// the caller needs to inspect headers or handle streaming responses.
// The caller is responsible for closing the response body.
func (c *Client) DoRaw(ctx context.Context, method, url string, body any, accept string) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.auth.Apply(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}
