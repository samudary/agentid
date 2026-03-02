package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/samudary/agentid/pkg/adapters"
)

// identifierRegex matches valid GitHub owner/repo names: alphanumeric, dots,
// hyphens, and underscores.
var identifierRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// validateIdentifier checks that a value is a valid GitHub identifier (owner
// or repo name). Rejects empty strings and values with path traversal,
// slashes, query strings, or other URL-special characters.
func validateIdentifier(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if !identifierRegex.MatchString(value) {
		return fmt.Errorf("%s contains invalid characters: %q (allowed: alphanumeric, dots, hyphens, underscores)", name, value)
	}
	return nil
}

// validateRef checks that a git ref (branch name, tag, SHA) doesn't contain
// path traversal sequences or null bytes. Allows forward slashes since branch
// names like "feature/foo" are valid.
func validateRef(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if strings.Contains(value, "..") {
		return fmt.Errorf("%s contains path traversal sequence: %q", name, value)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s contains null byte", name)
	}
	if strings.ContainsAny(value, "?#\\") {
		return fmt.Errorf("%s contains URL-special characters: %q", name, value)
	}
	return nil
}

// validatePath checks that a file path doesn't contain path traversal
// segments. Allows forward slashes (needed for directory paths like
// "src/main.go") but rejects any segment that is "..".
func validatePath(value string) error {
	if value == "" {
		return fmt.Errorf("path must not be empty")
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("path contains null byte")
	}
	if strings.ContainsAny(value, "?#\\") {
		return fmt.Errorf("path contains URL-special characters: %q", value)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return fmt.Errorf("path contains traversal segment '..': %q", value)
		}
	}
	return nil
}

// Adapter implements the adapters.Adapter interface for GitHub REST API.
type Adapter struct {
	baseURL    string // e.g., "https://api.github.com" or test server URL
	auth       adapters.UpstreamAuth
	httpClient *http.Client
}

// Satisfies adapters.Adapter.
var _ adapters.Adapter = (*Adapter)(nil)

// New creates a new GitHub adapter.
func New(baseURL string, auth adapters.UpstreamAuth) *Adapter {
	return &Adapter{
		baseURL:    strings.TrimRight(baseURL, "/"),
		auth:       auth,
		httpClient: &http.Client{},
	}
}

func (a *Adapter) Name() string { return "github" }

func (a *Adapter) Tools() []adapters.ToolDefinition {
	return []adapters.ToolDefinition{
		{
			Name:        "github_get_file",
			Description: "Get file contents from a GitHub repository",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"path": {"type": "string", "description": "File path"},
					"ref": {"type": "string", "description": "Branch or commit ref (optional)"}
				},
				"required": ["owner", "repo", "path"]
			}`),
		},
		{
			Name:        "github_create_branch",
			Description: "Create a new branch in a GitHub repository",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"branch": {"type": "string", "description": "New branch name"},
					"from_ref": {"type": "string", "description": "Base ref (default: main)"}
				},
				"required": ["owner", "repo", "branch"]
			}`),
		},
		{
			Name:        "github_create_pr",
			Description: "Create a pull request",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"title": {"type": "string", "description": "PR title"},
					"body": {"type": "string", "description": "PR description"},
					"head": {"type": "string", "description": "Source branch"},
					"base": {"type": "string", "description": "Target branch (default: main)"}
				},
				"required": ["owner", "repo", "title", "head"]
			}`),
		},
		{
			Name:        "github_get_ci_status",
			Description: "Get CI/commit status for a ref",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"ref": {"type": "string", "description": "Commit SHA or branch name"}
				},
				"required": ["owner", "repo", "ref"]
			}`),
		},
	}
}

func (a *Adapter) ScopeForTool(toolName string) string {
	switch toolName {
	case "github_get_file":
		return "github:repo:read"
	case "github_create_branch":
		return "github:repo:write"
	case "github_create_pr":
		return "github:pulls:write"
	case "github_get_ci_status":
		return "github:actions:read"
	default:
		return ""
	}
}

func (a *Adapter) Invoke(ctx context.Context, toolName string, input json.RawMessage) (*adapters.ToolResult, error) {
	switch toolName {
	case "github_get_file":
		return a.getFile(ctx, input)
	case "github_create_branch":
		return a.createBranch(ctx, input)
	case "github_create_pr":
		return a.createPR(ctx, input)
	case "github_get_ci_status":
		return a.getCIStatus(ctx, input)
	default:
		return nil, fmt.Errorf("unknown tool: %q", toolName)
	}
}

func (a *Adapter) getFile(ctx context.Context, input json.RawMessage) (*adapters.ToolResult, error) {
	var params struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Path  string `json:"path"`
		Ref   string `json:"ref"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	if err := validateIdentifier("owner", params.Owner); err != nil {
		return nil, err
	}
	if err := validateIdentifier("repo", params.Repo); err != nil {
		return nil, err
	}
	if err := validatePath(params.Path); err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s", a.baseURL, params.Owner, params.Repo, params.Path)
	if params.Ref != "" {
		if err := validateRef("ref", params.Ref); err != nil {
			return nil, err
		}
		reqURL += "?ref=" + url.QueryEscape(params.Ref)
	}

	return a.doRequest(ctx, http.MethodGet, reqURL, nil)
}

func (a *Adapter) createBranch(ctx context.Context, input json.RawMessage) (*adapters.ToolResult, error) {
	var params struct {
		Owner   string `json:"owner"`
		Repo    string `json:"repo"`
		Branch  string `json:"branch"`
		FromRef string `json:"from_ref"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	if err := validateIdentifier("owner", params.Owner); err != nil {
		return nil, err
	}
	if err := validateIdentifier("repo", params.Repo); err != nil {
		return nil, err
	}
	if err := validateRef("branch", params.Branch); err != nil {
		return nil, err
	}

	if params.FromRef == "" {
		params.FromRef = "main"
	}
	if err := validateRef("from_ref", params.FromRef); err != nil {
		return nil, err
	}

	// Step 1: Get the SHA of the base ref
	refURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", a.baseURL, params.Owner, params.Repo, params.FromRef)
	refResult, err := a.doRequest(ctx, http.MethodGet, refURL, nil)
	if err != nil {
		return nil, fmt.Errorf("get base ref: %w", err)
	}
	if refResult.IsError {
		return refResult, nil
	}

	// Parse the SHA from the response
	var refResp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal([]byte(refResult.Content[0].Text), &refResp); err != nil {
		return nil, fmt.Errorf("parse ref response: %w", err)
	}

	// Step 2: Create the new branch ref
	createURL := fmt.Sprintf("%s/repos/%s/%s/git/refs", a.baseURL, params.Owner, params.Repo)
	body := map[string]string{
		"ref": "refs/heads/" + params.Branch,
		"sha": refResp.Object.SHA,
	}

	return a.doRequest(ctx, http.MethodPost, createURL, body)
}

func (a *Adapter) createPR(ctx context.Context, input json.RawMessage) (*adapters.ToolResult, error) {
	var params struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	if err := validateIdentifier("owner", params.Owner); err != nil {
		return nil, err
	}
	if err := validateIdentifier("repo", params.Repo); err != nil {
		return nil, err
	}
	if err := validateRef("head", params.Head); err != nil {
		return nil, err
	}

	if params.Base == "" {
		params.Base = "main"
	}
	if err := validateRef("base", params.Base); err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/pulls", a.baseURL, params.Owner, params.Repo)
	body := map[string]string{
		"title": params.Title,
		"body":  params.Body,
		"head":  params.Head,
		"base":  params.Base,
	}

	return a.doRequest(ctx, http.MethodPost, reqURL, body)
}

func (a *Adapter) getCIStatus(ctx context.Context, input json.RawMessage) (*adapters.ToolResult, error) {
	var params struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Ref   string `json:"ref"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}

	if err := validateIdentifier("owner", params.Owner); err != nil {
		return nil, err
	}
	if err := validateIdentifier("repo", params.Repo); err != nil {
		return nil, err
	}
	if err := validateRef("ref", params.Ref); err != nil {
		return nil, err
	}

	reqURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s/status", a.baseURL, params.Owner, params.Repo, params.Ref)

	return a.doRequest(ctx, http.MethodGet, reqURL, nil)
}

// doRequest is a shared helper that builds, authenticates, sends, and reads
// an HTTP request, returning the result as a ToolResult.
func (a *Adapter) doRequest(ctx context.Context, method, url string, body any) (*adapters.ToolResult, error) {
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
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	a.auth.Apply(req)

	resp, err := a.httpClient.Do(req)
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
