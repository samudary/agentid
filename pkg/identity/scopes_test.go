package identity

import "testing"

func TestParseScope(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"github:repo:read", true},
		{"github:*:*", true},
		{"*:*:*", true},
		{"github:repo-name:read-write", true},
		{"github:repo", false},
		{"github::read", false},
		{"GitHub:Repo:Read", false},
		{"", false},
		{"a:b:c:d", false},
		{"github:repo:", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseScope(tt.input)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected error for invalid scope")
			}
		})
	}
}

func TestNarrows(t *testing.T) {
	tests := []struct {
		name     string
		child    string
		parent   string
		expected bool
	}{
		{"action narrowed from wildcard", "github:repo:read", "github:repo:*", true},
		{"action widened", "github:repo:write", "github:repo:read", false},
		{"resource and action narrowed", "github:repo:write", "github:*:*", true},
		{"different tool", "launchdarkly:flags:read", "github:repo:read", false},
		{"all narrowed from root", "github:repo:read", "*:*:*", true},
		{"action narrowed others wild", "*:*:read", "*:*:*", true},
		{"identical scope", "github:repo:read", "github:repo:read", true},
		{"resource widened", "github:*:read", "github:repo:*", false},
		{"root to root", "*:*:*", "*:*:*", true},
		{"partial wildcard narrowing", "*:*:read", "*:*:*", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Narrows(tt.child, tt.parent)
			if result != tt.expected {
				t.Errorf("Narrows(%q, %q) = %v, want %v", tt.child, tt.parent, result, tt.expected)
			}
		})
	}
}

func TestValidateScopes(t *testing.T) {
	tests := []struct {
		name      string
		requested []string
		parent    []string
		expectErr bool
	}{
		{"single valid", []string{"github:repo:read"}, []string{"github:repo:*"}, false},
		{"multiple valid", []string{"github:repo:read", "github:repo:write"}, []string{"github:repo:*"}, false},
		{"one invalid", []string{"github:repo:read", "launchdarkly:flags:write"}, []string{"github:repo:*"}, true},
		{"matched against different parents", []string{"github:repo:read", "launchdarkly:flags:write"}, []string{"github:repo:*", "launchdarkly:flags:*"}, false},
		{"empty requested", []string{}, []string{"github:repo:*"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScopes(tt.requested, tt.parent)
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestResolveBundles(t *testing.T) {
	bundles := map[string]BundleConfig{
		"code-contributor": {
			Description: "Read and write code",
			Scopes:      []string{"github:repo:read", "github:repo:write", "github:pulls:write"},
		},
		"code-reader": {
			Description: "Read-only code access",
			Scopes:      []string{"github:repo:read", "github:actions:read"},
		},
		"standard-feature-work": {
			Description: "Full feature development",
			Includes:    []string{"code-contributor"},
			Scopes:      []string{"launchdarkly:flags:create"},
		},
		"circular-a": {
			Includes: []string{"circular-b"},
		},
		"circular-b": {
			Includes: []string{"circular-a"},
		},
		"empty-bundle": {},
	}

	tests := []struct {
		name      string
		names     []string
		expectErr bool
		expected  []string // nil means don't check
	}{
		{"simple bundle", []string{"code-reader"}, false, []string{"github:actions:read", "github:repo:read"}},
		{"bundle with includes", []string{"standard-feature-work"}, false, []string{"github:pulls:write", "github:repo:read", "github:repo:write", "launchdarkly:flags:create"}},
		{"multiple bundles", []string{"code-reader", "code-contributor"}, false, []string{"github:actions:read", "github:pulls:write", "github:repo:read", "github:repo:write"}},
		{"circular includes", []string{"circular-a"}, true, nil},
		{"unknown bundle", []string{"nonexistent"}, true, nil},
		{"empty bundle", []string{"empty-bundle"}, false, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveBundles(tt.names, bundles)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.expected != nil {
				if len(result) != len(tt.expected) {
					t.Fatalf("expected %d scopes, got %d: %v", len(tt.expected), len(result), result)
				}
				for i, s := range result {
					if s != tt.expected[i] {
						t.Errorf("scope[%d] = %q, want %q", i, s, tt.expected[i])
					}
				}
			}
		})
	}
}
