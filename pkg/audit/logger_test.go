package audit_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samudary/agentid/pkg/audit"
	"github.com/samudary/agentid/pkg/store"
	"github.com/samudary/agentid/pkg/store/sqlite"
)

func setupLogger(t *testing.T) *audit.Logger {
	t.Helper()
	dir := t.TempDir()
	s, err := sqlite.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return audit.NewLogger(s)
}

func TestEmitAndQuery(t *testing.T) {
	logger := setupLogger(t)
	ctx := context.Background()

	taskID := "task-abc-123"
	chain := []store.DelegationLink{
		{Type: "human", ID: "jane@company.com", AuthorizedAt: time.Now().Unix(), ScopeNarrowed: false},
		{Type: "task", ID: taskID, AuthorizedAt: time.Now().Unix(), ScopeNarrowed: true},
	}

	events := []string{audit.EventTaskCreated, audit.EventToolInvoked, audit.EventTaskCompleted}
	// Truncate to second precision since SQLite stores timestamps as RFC3339
	// (second granularity). Without truncation, a sub-second before time
	// would be after the truncated stored timestamp.
	before := time.Now().UTC().Truncate(time.Second)

	for _, evt := range events {
		if err := logger.Emit(ctx, evt, taskID, chain, map[string]any{"action": evt}); err != nil {
			t.Fatalf("emit %s: %v", evt, err)
		}
	}

	results, err := logger.Query(ctx, store.EventFilter{TaskID: taskID})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 events, got %d", len(results))
	}

	for i, r := range results {
		if r.ID == "" {
			t.Errorf("event %d: ID is empty", i)
		}
		if r.Timestamp.Before(before) || r.Timestamp.After(before.Add(5*time.Second)) {
			t.Errorf("event %d: timestamp %v not within 5s of %v", i, r.Timestamp, before)
		}
		if r.Event != events[i] {
			t.Errorf("event %d: event = %q, want %q", i, r.Event, events[i])
		}
		if r.TaskID != taskID {
			t.Errorf("event %d: taskID = %q, want %q", i, r.TaskID, taskID)
		}
		if len(r.DelegationChain) != 2 {
			t.Errorf("event %d: expected 2 chain links, got %d", i, len(r.DelegationChain))
		} else {
			if r.DelegationChain[0].Type != "human" {
				t.Errorf("event %d: chain[0].Type = %q, want 'human'", i, r.DelegationChain[0].Type)
			}
			if r.DelegationChain[1].Type != "task" {
				t.Errorf("event %d: chain[1].Type = %q, want 'task'", i, r.DelegationChain[1].Type)
			}
		}
		if r.Payload["action"] != events[i] {
			t.Errorf("event %d: payload action = %v, want %q", i, r.Payload["action"], events[i])
		}
	}
}

func TestEmitWithPayload(t *testing.T) {
	logger := setupLogger(t)
	ctx := context.Background()

	payload := map[string]any{
		"tool":     "github-api",
		"endpoint": "/repos/owner/repo/pulls",
		"count":    float64(42), // Use float64 since JSON round-trips integers as float64
	}

	if err := logger.Emit(ctx, audit.EventToolInvoked, "task-xyz", nil, payload); err != nil {
		t.Fatalf("emit: %v", err)
	}

	results, err := logger.Query(ctx, store.EventFilter{TaskID: "task-xyz"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 event, got %d", len(results))
	}

	got := results[0].Payload
	if got["tool"] != "github-api" {
		t.Errorf("payload tool = %v, want 'github-api'", got["tool"])
	}
	if got["endpoint"] != "/repos/owner/repo/pulls" {
		t.Errorf("payload endpoint = %v, want '/repos/owner/repo/pulls'", got["endpoint"])
	}
	// JSON round-tripping converts integers to float64
	if got["count"] != float64(42) {
		t.Errorf("payload count = %v (%T), want float64(42)", got["count"], got["count"])
	}
}

func TestQueryByEventType(t *testing.T) {
	logger := setupLogger(t)
	ctx := context.Background()

	if err := logger.Emit(ctx, audit.EventTaskCreated, "task-1", nil, nil); err != nil {
		t.Fatalf("emit task.created: %v", err)
	}
	if err := logger.Emit(ctx, audit.EventToolInvoked, "task-1", nil, nil); err != nil {
		t.Fatalf("emit tool.invoked: %v", err)
	}
	if err := logger.Emit(ctx, audit.EventToolInvoked, "task-2", nil, nil); err != nil {
		t.Fatalf("emit tool.invoked 2: %v", err)
	}
	if err := logger.Emit(ctx, audit.EventTaskCompleted, "task-1", nil, nil); err != nil {
		t.Fatalf("emit task.completed: %v", err)
	}

	// Query only tool.invoked events
	results, err := logger.Query(ctx, store.EventFilter{Event: audit.EventToolInvoked})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 tool.invoked events, got %d", len(results))
	}
	for _, r := range results {
		if r.Event != audit.EventToolInvoked {
			t.Errorf("expected event %q, got %q", audit.EventToolInvoked, r.Event)
		}
	}

	// Query only task.created events
	results, err = logger.Query(ctx, store.EventFilter{Event: audit.EventTaskCreated})
	if err != nil {
		t.Fatalf("query task.created: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 task.created event, got %d", len(results))
	}
	if results[0].Event != audit.EventTaskCreated {
		t.Errorf("expected %q, got %q", audit.EventTaskCreated, results[0].Event)
	}
}

func TestQueryByTimeRange(t *testing.T) {
	logger := setupLogger(t)
	ctx := context.Background()

	// The SQLite store uses RFC3339 (second precision) for timestamps.
	// We need to sleep long enough to get distinct seconds for time range filtering.

	// Emit first event
	if err := logger.Emit(ctx, audit.EventTaskCreated, "task-time", nil, nil); err != nil {
		t.Fatalf("emit 1: %v", err)
	}

	// Sleep to ensure the next event has a different second
	time.Sleep(1100 * time.Millisecond)
	midpoint := time.Now().UTC()

	// Sleep again to ensure midpoint is strictly between events
	time.Sleep(1100 * time.Millisecond)

	// Emit second event
	if err := logger.Emit(ctx, audit.EventToolInvoked, "task-time", nil, nil); err != nil {
		t.Fatalf("emit 2: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)

	// Emit third event
	if err := logger.Emit(ctx, audit.EventTaskCompleted, "task-time", nil, nil); err != nil {
		t.Fatalf("emit 3: %v", err)
	}

	// Query events since midpoint: should get events 2 and 3
	results, err := logger.Query(ctx, store.EventFilter{
		TaskID: "task-time",
		Since:  midpoint,
	})
	if err != nil {
		t.Fatalf("query since: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 events since midpoint, got %d", len(results))
	}
	if results[0].Event != audit.EventToolInvoked {
		t.Errorf("first result event = %q, want %q", results[0].Event, audit.EventToolInvoked)
	}
	if results[1].Event != audit.EventTaskCompleted {
		t.Errorf("second result event = %q, want %q", results[1].Event, audit.EventTaskCompleted)
	}

	// Query events until midpoint: should get event 1
	results, err = logger.Query(ctx, store.EventFilter{
		TaskID: "task-time",
		Until:  midpoint,
	})
	if err != nil {
		t.Fatalf("query until: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 event until midpoint, got %d", len(results))
	}
	if results[0].Event != audit.EventTaskCreated {
		t.Errorf("result event = %q, want %q", results[0].Event, audit.EventTaskCreated)
	}
}

func TestEventConstants(t *testing.T) {
	constants := []string{
		audit.EventTaskCreated,
		audit.EventTaskCompleted,
		audit.EventTaskFailed,
		audit.EventTaskExpired,
		audit.EventCredentialIssued,
		audit.EventCredentialRevoked,
		audit.EventToolInvoked,
		audit.EventScopeDenied,
		audit.EventBundleResolved,
	}

	seen := make(map[string]bool)
	for _, c := range constants {
		if c == "" {
			t.Error("event constant is empty")
		}
		if seen[c] {
			t.Errorf("duplicate event constant: %q", c)
		}
		seen[c] = true
	}

	if len(seen) != 9 {
		t.Errorf("expected 9 unique event constants, got %d", len(seen))
	}
}
