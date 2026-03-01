package identity_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samudary/agentid/pkg/identity"
	"github.com/samudary/agentid/pkg/store/sqlite"
)

func setupService(t *testing.T) *identity.Service {
	t.Helper()
	dir := t.TempDir()
	s, err := sqlite.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	kp, err := identity.GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}

	cfg := identity.ServiceConfig{
		MaxTTL:             30 * time.Minute,
		MaxDelegationDepth: 5,
		Bundles: map[string]identity.BundleConfig{
			"code-contributor": {
				Description: "Read and write code",
				Scopes:      []string{"github:repo:read", "github:repo:write", "github:pulls:write"},
			},
		},
	}

	return identity.NewService(s, kp, cfg)
}

func TestCreateRootTask(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	cred, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:jane@company.com",
		Purpose:  "implement feature",
		Scopes:   []string{"github:repo:read", "github:repo:write"},
		TTL:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if cred.TaskID == "" {
		t.Error("task ID is empty")
	}
	if cred.JWT == "" {
		t.Error("JWT is empty")
	}
	if len(cred.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(cred.Scopes))
	}
	if len(cred.Chain) != 2 {
		t.Errorf("expected 2 chain links, got %d", len(cred.Chain))
	}
	if cred.Chain[0].Type != "human" {
		t.Errorf("first chain link type = %q, want 'human'", cred.Chain[0].Type)
	}
	if cred.Chain[1].Type != "task" {
		t.Errorf("second chain link type = %q, want 'task'", cred.Chain[1].Type)
	}
}

func TestCreateRootTaskWithBundle(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	cred, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:jane@company.com",
		Purpose:  "feature work",
		Bundles:  []string{"code-contributor"},
		TTL:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if len(cred.Scopes) != 3 {
		t.Errorf("expected 3 scopes from bundle, got %d: %v", len(cred.Scopes), cred.Scopes)
	}
}

// Test: Create sub-task with narrowed scopes
func TestCreateSubTask(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	parent, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:jane@company.com",
		Purpose:  "parent task",
		Scopes:   []string{"github:repo:read", "github:repo:write"},
		TTL:      30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	child, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: parent.TaskID,
		Purpose:  "child read-only",
		Scopes:   []string{"github:repo:read"},
		TTL:      5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if len(child.Chain) != 3 {
		t.Errorf("expected 3 chain links, got %d", len(child.Chain))
	}
	if len(child.Scopes) != 1 {
		t.Errorf("expected 1 scope, got %d", len(child.Scopes))
	}
}

func TestSubTaskScopeWideningRejected(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	parent, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:jane@company.com",
		Purpose:  "parent",
		Scopes:   []string{"github:repo:read"},
		TTL:      30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	_, err = svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: parent.TaskID,
		Purpose:  "child wants write",
		Scopes:   []string{"github:repo:write"}, // parent only has read!
		TTL:      5 * time.Minute,
	})
	if err == nil {
		t.Fatal("expected error for scope widening, got nil")
	}
}

func TestValidateAndRevoke(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	cred, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:jane@company.com",
		Purpose:  "test revocation",
		Scopes:   []string{"github:repo:read"},
		TTL:      10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	claims, err := svc.ValidateCredential(ctx, cred.JWT)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.Subject != cred.TaskID {
		t.Errorf("subject = %q, want %q", claims.Subject, cred.TaskID)
	}

	if err := svc.RevokeTask(ctx, cred.TaskID, "test done"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Validate should fail
	_, err = svc.ValidateCredential(ctx, cred.JWT)
	if err == nil {
		t.Fatal("expected error after revocation, got nil")
	}
}

func TestTTLCapped(t *testing.T) {
	svc := setupService(t)
	ctx := context.Background()

	cred, err := svc.CreateTask(ctx, identity.TaskRequest{
		ParentID: "human:jane@company.com",
		Purpose:  "long running",
		Scopes:   []string{"github:repo:read"},
		TTL:      24 * time.Hour, // way over max of 30m
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Should be capped to 30 minutes
	maxExpected := time.Now().Add(31 * time.Minute)
	if cred.ExpiresAt.After(maxExpected) {
		t.Errorf("TTL not capped: expires at %v, expected before %v", cred.ExpiresAt, maxExpected)
	}
}
