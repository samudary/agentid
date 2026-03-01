package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/samudary/agentid/pkg/store"
)

// Logger provides structured audit event logging backed by a Store.
type Logger struct {
	store store.Store
}

// NewLogger creates a new audit logger.
func NewLogger(s store.Store) *Logger {
	return &Logger{store: s}
}

// Emit records an audit event. It auto-generates the event ID (UUIDv7) and
// timestamp. The delegation chain provides authorization provenance context.
func (l *Logger) Emit(ctx context.Context, event string, taskID string, chain []store.DelegationLink, payload map[string]any) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, &store.AuditEvent{
		ID:              id.String(),
		Timestamp:       time.Now().UTC(),
		Event:           event,
		TaskID:          taskID,
		DelegationChain: chain,
		Payload:         payload,
	})
}

// Query retrieves audit events matching the given filter.
func (l *Logger) Query(ctx context.Context, filter store.EventFilter) ([]*store.AuditEvent, error) {
	return l.store.QueryEvents(ctx, filter)
}
