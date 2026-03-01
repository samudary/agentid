package store

import (
	"context"
	"errors"
	"time"
)

// ErrTaskNotFound is returned when a task lookup finds no matching record.
var ErrTaskNotFound = errors.New("task not found")

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusActive    TaskStatus = "active"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusExpired   TaskStatus = "expired"
	TaskStatusRevoked   TaskStatus = "revoked"
)

// DelegationLink represents a single link in a task's delegation chain,
// tracing authorization from the human root through intermediate tasks.
type DelegationLink struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	AuthorizedAt  int64  `json:"authorized_at"`
	ScopeNarrowed bool   `json:"scope_narrowed"`
}

// TaskRecord is the persistent representation of a task identity.
type TaskRecord struct {
	ID              string
	ParentID        string
	Purpose         string
	Scopes          []string
	Status          TaskStatus
	DelegationChain []DelegationLink
	Metadata        map[string]string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	CompletedAt     *time.Time
	StatusReason    string
}

// AuditEvent is an immutable, append-only record of something that happened
// in the system.
type AuditEvent struct {
	ID              string
	Timestamp       time.Time
	Event           string
	TaskID          string
	DelegationChain []DelegationLink
	Payload         map[string]any
}

// EventFilter controls which audit events are returned by QueryEvents.
type EventFilter struct {
	TaskID     string
	Event      string
	Authorizer string
	Since      time.Time
	Until      time.Time
	Limit      int
}

// Store defines the storage interface for AgentID task identity records,
// revocation state, and audit events.
//
// Design constraints:
//   - No recursive queries: delegation chains are denormalized at write time
//   - Revocation is a point lookup: O(1), not a list scan
//   - Append-only audit writes: events are immutable
//   - Context propagation: every method takes context
type Store interface {
	// CreateTask persists a new task record. The task ID must be unique.
	CreateTask(ctx context.Context, task *TaskRecord) error

	// GetTask retrieves a task by ID. Returns ErrTaskNotFound if the task
	// does not exist.
	GetTask(ctx context.Context, taskID string) (*TaskRecord, error)

	// UpdateTaskStatus transitions a task to a new status with an optional
	// reason. When the new status is Completed or Failed, CompletedAt is
	// set to the current time.
	UpdateTaskStatus(ctx context.Context, taskID string, status TaskStatus, reason string) error

	// IsRevoked returns true if the task has been explicitly revoked.
	// Returns false (not an error) for tasks that do not exist or have
	// not been revoked.
	IsRevoked(ctx context.Context, taskID string) (bool, error)

	// RevokeTask marks a task as revoked by inserting into the revocation
	// index AND updating the task's status to Revoked.
	RevokeTask(ctx context.Context, taskID string, revokedAt time.Time, reason string) error

	// GetDelegationChain returns the denormalized delegation chain for a task.
	GetDelegationChain(ctx context.Context, taskID string) ([]DelegationLink, error)

	// AppendEvent writes an immutable audit event.
	AppendEvent(ctx context.Context, event *AuditEvent) error

	// QueryEvents returns audit events matching the given filter, ordered
	// by timestamp ascending.
	QueryEvents(ctx context.Context, filter EventFilter) ([]*AuditEvent, error)

	// Close releases any resources held by the store.
	Close() error

	// Ping verifies that the store is reachable and operational.
	Ping(ctx context.Context) error
}
