package identity

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Scope represents a parsed tool:resource:action permission.
type Scope struct {
	Tool     string
	Resource string
	Action   string
}

var segmentRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// ParseScope parses a scope string into its three segments.
// Valid: "github:repo:read", "github:*:*", "*:*:*"
// Invalid: "github:repo" (wrong segments), "GitHub:Repo:Read" (uppercase), "" (empty)
func ParseScope(s string) (Scope, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return Scope{}, fmt.Errorf("scope must have 3 colon-separated segments, got %d: %q", len(parts), s)
	}
	for i, p := range parts {
		if p == "" {
			return Scope{}, fmt.Errorf("scope segment %d is empty in %q", i, s)
		}
		if p != "*" && !segmentRegex.MatchString(p) {
			return Scope{}, fmt.Errorf("scope segment %q must be lowercase alphanumeric/hyphens or '*'", p)
		}
	}
	return Scope{Tool: parts[0], Resource: parts[1], Action: parts[2]}, nil
}

// String returns the scope in tool:resource:action format.
func (s Scope) String() string {
	return s.Tool + ":" + s.Resource + ":" + s.Action
}

// Narrows returns true if child is a valid narrowing of parent.
// For each segment: child[i] == parent[i] OR parent[i] == "*"
func Narrows(child, parent string) bool {
	c, err := ParseScope(child)
	if err != nil {
		return false
	}
	p, err := ParseScope(parent)
	if err != nil {
		return false
	}
	return (c.Tool == p.Tool || p.Tool == "*") &&
		(c.Resource == p.Resource || p.Resource == "*") &&
		(c.Action == p.Action || p.Action == "*")
}

// ValidateScopes checks that every requested scope is a valid narrowing
// of at least one parent scope.
func ValidateScopes(requested, parent []string) error {
	for _, req := range requested {
		if _, err := ParseScope(req); err != nil {
			return fmt.Errorf("invalid scope: %w", err)
		}
		found := false
		for _, p := range parent {
			if Narrows(req, p) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("scope %q is not a valid narrowing of any parent scope", req)
		}
	}
	return nil
}

// BundleConfig defines a named group of scopes.
type BundleConfig struct {
	Description string   `json:"description" yaml:"description"`
	Scopes      []string `json:"scopes" yaml:"scopes"`
	Includes    []string `json:"includes" yaml:"includes"`
}

// ValidateBundleConfigs checks all bundle definitions for errors at startup.
// It rejects empty bundles, validates scope format, detects unknown include
// references, and detects circular includes.
func ValidateBundleConfigs(bundles map[string]BundleConfig) error {
	for name, b := range bundles {
		if len(b.Scopes) == 0 && len(b.Includes) == 0 {
			return fmt.Errorf("bundle %q must have at least one of scopes or includes", name)
		}
		for _, s := range b.Scopes {
			if _, err := ParseScope(s); err != nil {
				return fmt.Errorf("bundle %q: %w", name, err)
			}
		}
		for _, inc := range b.Includes {
			if _, ok := bundles[inc]; !ok {
				return fmt.Errorf("bundle %q includes unknown bundle %q", name, inc)
			}
		}
	}

	// Detect circular includes by trial-expanding every bundle
	for name := range bundles {
		if _, err := ResolveBundles([]string{name}, bundles); err != nil {
			return err
		}
	}
	return nil
}

// ResolveBundles expands bundle names into granular scopes.
// Detects circular includes. Returns a deduplicated, sorted scope list.
func ResolveBundles(names []string, bundles map[string]BundleConfig) ([]string, error) {
	seen := make(map[string]bool)     // for cycle detection
	resolved := make(map[string]bool) // for deduplication

	var resolve func(name string) error
	resolve = func(name string) error {
		if seen[name] {
			return fmt.Errorf("circular bundle include detected: %q", name)
		}
		b, ok := bundles[name]
		if !ok {
			return fmt.Errorf("unknown bundle: %q", name)
		}
		seen[name] = true

		// Resolve includes first (depth-first)
		for _, inc := range b.Includes {
			if err := resolve(inc); err != nil {
				return err
			}
		}

		// Add this bundle's scopes
		for _, s := range b.Scopes {
			resolved[s] = true
		}

		delete(seen, name) // allow same bundle from different paths
		return nil
	}

	for _, name := range names {
		if err := resolve(name); err != nil {
			return nil, err
		}
	}

	// Convert to sorted slice
	result := make([]string, 0, len(resolved))
	for s := range resolved {
		result = append(result, s)
	}
	sort.Strings(result)
	return result, nil
}
