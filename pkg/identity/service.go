package identity

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/samudary/agentid/pkg/store"
)

// TaskRequest contains the parameters for creating a new task identity.
type TaskRequest struct {
	ParentID string            // "human:<email>" for root tasks, or "task:<uuidv7>" for sub-tasks
	Purpose  string            // Human-readable, informational only
	Scopes   []string          // Granular tool:resource:action permissions
	Bundles  []string          // Named scope bundles (expanded before validation)
	TTL      time.Duration     // Requested credential lifetime
	Metadata map[string]string // Arbitrary context for policy evaluation
}

// TaskCredential is returned after successful task creation.
type TaskCredential struct {
	TaskID    string
	JWT       string
	Scopes    []string
	ExpiresAt time.Time
	Chain     []store.DelegationLink
}

// ServiceConfig configures the identity service.
type ServiceConfig struct {
	MaxTTL             time.Duration
	MaxDelegationDepth int
	Bundles            map[string]BundleConfig
}

// Service manages the task identity lifecycle.
type Service struct {
	store   store.Store
	keyPair *KeyPair
	config  ServiceConfig
}

// NewService creates a new identity service.
func NewService(s store.Store, kp *KeyPair, cfg ServiceConfig) *Service {
	return &Service{store: s, keyPair: kp, config: cfg}
}

// CreateTask creates a new task identity, validates scopes, builds the
// delegation chain, signs a JWT, and persists the task record.
func (svc *Service) CreateTask(ctx context.Context, req TaskRequest) (*TaskCredential, error) {
	// 1. Resolve bundles to granular scopes
	allScopes := make([]string, len(req.Scopes))
	copy(allScopes, req.Scopes)

	if len(req.Bundles) > 0 {
		bundleScopes, err := ResolveBundles(req.Bundles, svc.config.Bundles)
		if err != nil {
			return nil, fmt.Errorf("resolve bundles: %w", err)
		}
		allScopes = append(allScopes, bundleScopes...)
	}

	allScopes = deduplicateScopes(allScopes)

	// Validate all scopes are well-formed
	for _, s := range allScopes {
		if _, err := ParseScope(s); err != nil {
			return nil, fmt.Errorf("invalid scope: %w", err)
		}
	}

	// 2. Determine parent context, validate scope narrowing, and derive delegation depth
	var parentTask *store.TaskRecord
	var parentChain []store.DelegationLink
	var maxDepth int

	if strings.HasPrefix(req.ParentID, "human:") {
		maxDepth = svc.config.MaxDelegationDepth
	} else if strings.HasPrefix(req.ParentID, "task:") {
		var err error
		parentTask, err = svc.store.GetTask(ctx, req.ParentID)
		if err != nil {
			return nil, fmt.Errorf("get parent task: %w", err)
		}
		if parentTask.Status != store.TaskStatusActive {
			return nil, fmt.Errorf("parent task %q is not active (status: %s)", req.ParentID, parentTask.Status)
		}
		if parentTask.MaxDelegationDepth <= 0 {
			return nil, fmt.Errorf("parent task %q has exhausted its delegation depth", req.ParentID)
		}

		if err := ValidateScopes(allScopes, parentTask.Scopes); err != nil {
			return nil, fmt.Errorf("scope narrowing violation: %w", err)
		}

		parentChain = parentTask.DelegationChain
		maxDepth = parentTask.MaxDelegationDepth - 1
	} else {
		return nil, fmt.Errorf("invalid parent ID format: %q (must start with 'human:' or 'task:')", req.ParentID)
	}

	// 3. Enforce TTL limits
	ttl := req.TTL
	if ttl <= 0 {
		ttl = svc.config.MaxTTL
	}
	if ttl > svc.config.MaxTTL {
		ttl = svc.config.MaxTTL
	}
	if parentTask != nil {
		remaining := time.Until(parentTask.ExpiresAt)
		if remaining <= 0 {
			return nil, fmt.Errorf("parent task has expired")
		}
		if ttl > remaining {
			ttl = remaining
		}
	}

	// 4. Generate task ID (UUIDv7)
	taskUUID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate task ID: %w", err)
	}
	taskID := "task:" + taskUUID.String()

	// 5. Build delegation chain
	scopeNarrowed := false
	if parentTask != nil {
		scopeNarrowed = !scopeSetsEqual(allScopes, parentTask.Scopes)
	}

	chain, err := BuildDelegationChain(taskID, req.ParentID, parentChain, scopeNarrowed)
	if err != nil {
		return nil, fmt.Errorf("build delegation chain: %w", err)
	}

	// 6. Build JWT claims and sign
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	claims := TaskClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "agentid",
			Subject:   taskID,
			ID:        taskUUID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Purpose:            req.Purpose,
		Scopes:             allScopes,
		DelegationChain:    chain,
		PolicyContext:      req.Metadata,
		MaxDelegationDepth: maxDepth,
		MaxTTLSeconds:      int(ttl.Seconds()),
	}

	tokenString, err := SignToken(claims, svc.keyPair.Private)
	if err != nil {
		return nil, fmt.Errorf("sign JWT: %w", err)
	}

	// 7. Persist task record
	record := &store.TaskRecord{
		ID:                 taskID,
		ParentID:           req.ParentID,
		Purpose:            req.Purpose,
		Scopes:             allScopes,
		Status:             store.TaskStatusActive,
		DelegationChain:    chain,
		Metadata:           req.Metadata,
		MaxDelegationDepth: maxDepth,
		CreatedAt:          now,
		ExpiresAt:          expiresAt,
	}
	if record.Metadata == nil {
		record.Metadata = make(map[string]string)
	}

	if err := svc.store.CreateTask(ctx, record); err != nil {
		return nil, fmt.Errorf("persist task: %w", err)
	}

	return &TaskCredential{
		TaskID:    taskID,
		JWT:       tokenString,
		Scopes:    allScopes,
		ExpiresAt: expiresAt,
		Chain:     chain,
	}, nil
}

// ValidateCredential verifies a JWT and checks revocation status.
func (svc *Service) ValidateCredential(ctx context.Context, tokenString string) (*TaskClaims, error) {
	// 1. Verify signature and standard claims (exp, iss)
	claims, err := VerifyToken(tokenString, svc.keyPair.Public)
	if err != nil {
		return nil, err
	}

	// 2. Check revocation using the full task ID (sub claim)
	revoked, err := svc.store.IsRevoked(ctx, claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("check revocation: %w", err)
	}
	if revoked {
		return nil, fmt.Errorf("task %q has been revoked", claims.Subject)
	}

	return claims, nil
}

// RevokeTask marks a task as revoked.
func (svc *Service) RevokeTask(ctx context.Context, taskID string, reason string) error {
	return svc.store.RevokeTask(ctx, taskID, time.Now().UTC(), reason)
}

// GetTask retrieves a task record from the store.
func (svc *Service) GetTask(ctx context.Context, taskID string) (*store.TaskRecord, error) {
	return svc.store.GetTask(ctx, taskID)
}

// GetDelegationChain returns the delegation chain for a task.
func (svc *Service) GetDelegationChain(ctx context.Context, taskID string) ([]store.DelegationLink, error) {
	return svc.store.GetDelegationChain(ctx, taskID)
}

// helper: deduplicate and sort scopes
func deduplicateScopes(scopes []string) []string {
	seen := make(map[string]bool, len(scopes))
	result := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	sort.Strings(result)
	return result
}

// helper: check if two scope sets are identical
func scopeSetsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := make([]string, len(a))
	bs := make([]string, len(b))
	copy(as, a)
	copy(bs, b)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
