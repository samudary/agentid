// Package sqlite provides a SQLite-backed implementation of the store.Store
// interface using https://pkg.go.dev/modernc.org/sqlite
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/samudary/agentid/pkg/store"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements store.Store backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// Compile-time check that SQLiteStore satisfies store.Store.
var _ store.Store = (*SQLiteStore)(nil)

// New opens (or creates) a SQLite database at the given path, enables WAL
// mode, and applies the schema.
func New(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite WAL pragma: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Ping verifies the database connection is alive.
func (s *SQLiteStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// CreateTask inserts a new task record. Scopes, DelegationChain, and
// Metadata are serialized as JSON text columns.
func (s *SQLiteStore) CreateTask(ctx context.Context, task *store.TaskRecord) error {
	scopesJSON, err := json.Marshal(task.Scopes)
	if err != nil {
		return fmt.Errorf("marshal scopes: %w", err)
	}

	chainJSON, err := json.Marshal(task.DelegationChain)
	if err != nil {
		return fmt.Errorf("marshal delegation_chain: %w", err)
	}

	metaJSON, err := json.Marshal(task.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	var completedAt *string
	if task.CompletedAt != nil {
		s := task.CompletedAt.UTC().Format(time.RFC3339)
		completedAt = &s
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks (
			id, parent_id, purpose, scopes, status,
			delegation_chain, metadata, max_delegation_depth,
			created_at, expires_at, completed_at, status_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.ParentID,
		task.Purpose,
		string(scopesJSON),
		string(task.Status),
		string(chainJSON),
		string(metaJSON),
		task.MaxDelegationDepth,
		task.CreatedAt.UTC().Format(time.RFC3339),
		task.ExpiresAt.UTC().Format(time.RFC3339),
		completedAt,
		task.StatusReason,
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// GetTask retrieves a task by ID. Returns store.ErrTaskNotFound if the task
// does not exist.
func (s *SQLiteStore) GetTask(ctx context.Context, taskID string) (*store.TaskRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, parent_id, purpose, scopes, status,
		       delegation_chain, metadata, max_delegation_depth,
		       created_at, expires_at, completed_at, status_reason
		FROM tasks WHERE id = ?`, taskID)

	var (
		task          store.TaskRecord
		scopesJSON    string
		chainJSON     string
		metaJSON      string
		createdAtStr  string
		expiresAtStr  string
		completedAtDB sql.NullString
		statusStr     string
	)

	err := row.Scan(
		&task.ID,
		&task.ParentID,
		&task.Purpose,
		&scopesJSON,
		&statusStr,
		&chainJSON,
		&metaJSON,
		&task.MaxDelegationDepth,
		&createdAtStr,
		&expiresAtStr,
		&completedAtDB,
		&task.StatusReason,
	)
	if err == sql.ErrNoRows {
		return nil, store.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}

	task.Status = store.TaskStatus(statusStr)

	if err := json.Unmarshal([]byte(scopesJSON), &task.Scopes); err != nil {
		return nil, fmt.Errorf("unmarshal scopes: %w", err)
	}

	if err := json.Unmarshal([]byte(chainJSON), &task.DelegationChain); err != nil {
		return nil, fmt.Errorf("unmarshal delegation_chain: %w", err)
	}

	if err := json.Unmarshal([]byte(metaJSON), &task.Metadata); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	task.CreatedAt, err = time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	task.ExpiresAt, err = time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		return nil, fmt.Errorf("parse expires_at: %w", err)
	}

	if completedAtDB.Valid {
		t, err := time.Parse(time.RFC3339, completedAtDB.String)
		if err != nil {
			return nil, fmt.Errorf("parse completed_at: %w", err)
		}
		task.CompletedAt = &t
	}

	return &task, nil
}

// UpdateTaskStatus transitions a task to a new status with an optional
// reason. When the status is Completed or Failed, CompletedAt is set to
// the current time.
func (s *SQLiteStore) UpdateTaskStatus(ctx context.Context, taskID string, status store.TaskStatus, reason string) error {
	var completedAt *string
	if status == store.TaskStatusCompleted || status == store.TaskStatusFailed {
		now := time.Now().UTC().Format(time.RFC3339)
		completedAt = &now
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, status_reason = ?, completed_at = COALESCE(?, completed_at)
		WHERE id = ?`,
		string(status), reason, completedAt, taskID,
	)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return store.ErrTaskNotFound
	}
	return nil
}

// IsRevoked returns true if the task has been explicitly revoked.
// Returns false (not an error) for tasks that do not exist in the
// revocations table.
func (s *SQLiteStore) IsRevoked(ctx context.Context, taskID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM revocations WHERE task_id = ?`, taskID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("is revoked: %w", err)
	}
	return count > 0, nil
}

// RevokeTask inserts a revocation record and updates the task status to
// Revoked within a single transaction.
func (s *SQLiteStore) RevokeTask(ctx context.Context, taskID string, revokedAt time.Time, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO revocations (task_id, revoked_at, reason)
		VALUES (?, ?, ?)`,
		taskID, revokedAt.UTC().Format(time.RFC3339), reason,
	)
	if err != nil {
		return fmt.Errorf("insert revocation: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, status_reason = ?
		WHERE id = ?`,
		string(store.TaskStatusRevoked), reason, taskID,
	)
	if err != nil {
		return fmt.Errorf("update task to revoked: %w", err)
	}

	return tx.Commit()
}

// GetDelegationChain reads the denormalized delegation chain from the tasks
// table for the given task ID.
func (s *SQLiteStore) GetDelegationChain(ctx context.Context, taskID string) ([]store.DelegationLink, error) {
	var chainJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT delegation_chain FROM tasks WHERE id = ?`, taskID,
	).Scan(&chainJSON)
	if err == sql.ErrNoRows {
		return nil, store.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get delegation chain: %w", err)
	}

	var chain []store.DelegationLink
	if err := json.Unmarshal([]byte(chainJSON), &chain); err != nil {
		return nil, fmt.Errorf("unmarshal delegation_chain: %w", err)
	}
	return chain, nil
}

// AppendEvent writes an immutable audit event to the audit_events table.
func (s *SQLiteStore) AppendEvent(ctx context.Context, event *store.AuditEvent) error {
	chainJSON, err := json.Marshal(event.DelegationChain)
	if err != nil {
		return fmt.Errorf("marshal delegation_chain: %w", err)
	}

	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO audit_events (id, timestamp, event, task_id, delegation_chain, payload)
		VALUES (?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.Timestamp.UTC().Format(time.RFC3339),
		event.Event,
		event.TaskID,
		string(chainJSON),
		string(payloadJSON),
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// QueryEvents returns audit events matching the given filter, ordered by
// timestamp ascending.
func (s *SQLiteStore) QueryEvents(ctx context.Context, filter store.EventFilter) ([]*store.AuditEvent, error) {
	var (
		clauses []string
		args    []any
	)

	if filter.TaskID != "" {
		clauses = append(clauses, "task_id = ?")
		args = append(args, filter.TaskID)
	}
	if filter.Event != "" {
		clauses = append(clauses, "event = ?")
		args = append(args, filter.Event)
	}
	if filter.Authorizer != "" {
		// Search the delegation_chain JSON text for the authorizer string.
		clauses = append(clauses, "delegation_chain LIKE ?")
		args = append(args, "%"+filter.Authorizer+"%")
	}
	if !filter.Since.IsZero() {
		clauses = append(clauses, "timestamp >= ?")
		args = append(args, filter.Since.UTC().Format(time.RFC3339))
	}
	if !filter.Until.IsZero() {
		clauses = append(clauses, "timestamp <= ?")
		args = append(args, filter.Until.UTC().Format(time.RFC3339))
	}

	query := "SELECT id, timestamp, event, task_id, delegation_chain, payload FROM audit_events"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY timestamp ASC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []*store.AuditEvent
	for rows.Next() {
		var (
			evt          store.AuditEvent
			timestampStr string
			chainJSON    string
			payloadJSON  string
		)

		if err := rows.Scan(&evt.ID, &timestampStr, &evt.Event, &evt.TaskID, &chainJSON, &payloadJSON); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		evt.Timestamp, err = time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp: %w", err)
		}

		if err := json.Unmarshal([]byte(chainJSON), &evt.DelegationChain); err != nil {
			return nil, fmt.Errorf("unmarshal delegation_chain: %w", err)
		}

		if err := json.Unmarshal([]byte(payloadJSON), &evt.Payload); err != nil {
			return nil, fmt.Errorf("unmarshal payload: %w", err)
		}

		events = append(events, &evt)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}

	return events, nil
}
