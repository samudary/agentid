// Package storetest provides a reusable conformance test suite for any
// implementation of the store.Store interface. Backend-specific test files
// call TestStore(t, s) with a concrete Store to verify correctness.
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/samudary/agentid/pkg/store"
)

// TestStore runs the full conformance suite against the provided Store.
func TestStore(t *testing.T, s store.Store) {
	t.Run("Ping", func(t *testing.T) { testPing(t, s) })
	t.Run("CreateAndGetTask", func(t *testing.T) { testCreateAndGetTask(t, s) })
	t.Run("GetTaskNotFound", func(t *testing.T) { testGetTaskNotFound(t, s) })
	t.Run("UpdateTaskStatus", func(t *testing.T) { testUpdateTaskStatus(t, s) })
	t.Run("RevokeAndCheck", func(t *testing.T) { testRevokeAndCheck(t, s) })
	t.Run("IsRevokedNotRevoked", func(t *testing.T) { testIsRevokedNotRevoked(t, s) })
	t.Run("DelegationChain", func(t *testing.T) { testDelegationChain(t, s) })
	t.Run("AppendAndQueryEvents", func(t *testing.T) { testAppendAndQueryEvents(t, s) })
	t.Run("QueryEventsFilters", func(t *testing.T) { testQueryEventsFilters(t, s) })
}

// testPing verifies that the store is reachable.
func testPing(t *testing.T, s store.Store) {
	ctx := context.Background()
	if err := s.Ping(ctx); err != nil {
		t.Fatalf("Ping returned error: %v", err)
	}
}

// testCreateAndGetTask creates a fully-populated TaskRecord, persists it,
// retrieves it, and verifies every field round-trips correctly.
func testCreateAndGetTask(t *testing.T, s store.Store) {
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(30 * time.Minute)

	task := &store.TaskRecord{
		ID:       "task-create-get-001",
		ParentID: "task-parent-001",
		Purpose:  "implement dark-launch feature flag",
		Scopes:   []string{"github:repo:write", "launchdarkly:flags:create"},
		Status:   store.TaskStatusActive,
		DelegationChain: []store.DelegationLink{
			{
				Type:          "human",
				ID:            "human:jane@company.com",
				AuthorizedAt:  now.Unix() - 100,
				ScopeNarrowed: false,
			},
			{
				Type:          "task",
				ID:            "task:01961c88-a3b7-7de1-bd35-4f22e384a080",
				AuthorizedAt:  now.Unix(),
				ScopeNarrowed: true,
			},
		},
		Metadata: map[string]string{
			"team":    "platform",
			"project": "payments-v2",
		},
		CreatedAt:    now,
		ExpiresAt:    expires,
		CompletedAt:  nil,
		StatusReason: "",
	}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	assertEqual(t, "ID", task.ID, got.ID)
	assertEqual(t, "ParentID", task.ParentID, got.ParentID)
	assertEqual(t, "Purpose", task.Purpose, got.Purpose)
	assertEqual(t, "Status", string(task.Status), string(got.Status))
	assertEqual(t, "StatusReason", task.StatusReason, got.StatusReason)

	if !got.CreatedAt.Equal(task.CreatedAt) {
		t.Errorf("CreatedAt: want %v, got %v", task.CreatedAt, got.CreatedAt)
	}
	if !got.ExpiresAt.Equal(task.ExpiresAt) {
		t.Errorf("ExpiresAt: want %v, got %v", task.ExpiresAt, got.ExpiresAt)
	}
	if got.CompletedAt != nil {
		t.Errorf("CompletedAt: want nil, got %v", got.CompletedAt)
	}

	if len(got.Scopes) != len(task.Scopes) {
		t.Fatalf("Scopes length: want %d, got %d", len(task.Scopes), len(got.Scopes))
	}
	for i, s := range task.Scopes {
		assertEqual(t, "Scopes["+string(rune('0'+i))+"]", s, got.Scopes[i])
	}

	if len(got.DelegationChain) != len(task.DelegationChain) {
		t.Fatalf("DelegationChain length: want %d, got %d", len(task.DelegationChain), len(got.DelegationChain))
	}
	for i, link := range task.DelegationChain {
		assertDelegationLink(t, i, link, got.DelegationChain[i])
	}

	if len(got.Metadata) != len(task.Metadata) {
		t.Fatalf("Metadata length: want %d, got %d", len(task.Metadata), len(got.Metadata))
	}
	for k, v := range task.Metadata {
		if got.Metadata[k] != v {
			t.Errorf("Metadata[%q]: want %q, got %q", k, v, got.Metadata[k])
		}
	}
}

// testGetTaskNotFound verifies that GetTask returns an error for a
// nonexistent task ID.
func testGetTaskNotFound(t *testing.T, s store.Store) {
	ctx := context.Background()

	_, err := s.GetTask(ctx, "nonexistent-task-id")
	if err == nil {
		t.Fatal("GetTask for nonexistent ID should return error, got nil")
	}
}

// testUpdateTaskStatus creates a task, transitions it to Completed with a
// reason, and verifies the status, reason, and CompletedAt fields.
func testUpdateTaskStatus(t *testing.T, s store.Store) {
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	task := &store.TaskRecord{
		ID:        "task-update-status-001",
		Purpose:   "status update test",
		Scopes:    []string{"github:repo:read"},
		Status:    store.TaskStatusActive,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
		Metadata:  map[string]string{},
	}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	reason := "all work finished successfully"
	if err := s.UpdateTaskStatus(ctx, task.ID, store.TaskStatusCompleted, reason); err != nil {
		t.Fatalf("UpdateTaskStatus failed: %v", err)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	assertEqual(t, "Status", string(store.TaskStatusCompleted), string(got.Status))
	assertEqual(t, "StatusReason", reason, got.StatusReason)

	if got.CompletedAt == nil {
		t.Fatal("CompletedAt should be set after status transition to Completed")
	}
}

// testRevokeAndCheck creates a task, confirms it is not revoked, revokes it,
// then confirms it is revoked.
func testRevokeAndCheck(t *testing.T, s store.Store) {
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	task := &store.TaskRecord{
		ID:        "task-revoke-001",
		Purpose:   "revocation test",
		Scopes:    []string{"github:repo:read"},
		Status:    store.TaskStatusActive,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
		Metadata:  map[string]string{},
	}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	revoked, err := s.IsRevoked(ctx, task.ID)
	if err != nil {
		t.Fatalf("IsRevoked failed: %v", err)
	}
	if revoked {
		t.Fatal("IsRevoked should return false before revocation")
	}

	if err := s.RevokeTask(ctx, task.ID, now, "policy violation"); err != nil {
		t.Fatalf("RevokeTask failed: %v", err)
	}

	revoked, err = s.IsRevoked(ctx, task.ID)
	if err != nil {
		t.Fatalf("IsRevoked after revocation failed: %v", err)
	}
	if !revoked {
		t.Fatal("IsRevoked should return true after revocation")
	}
}

// testIsRevokedNotRevoked verifies that IsRevoked returns false (not an
// error) for a task that was never revoked or does not exist.
func testIsRevokedNotRevoked(t *testing.T, s store.Store) {
	ctx := context.Background()

	revoked, err := s.IsRevoked(ctx, "never-revoked-task-id")
	if err != nil {
		t.Fatalf("IsRevoked for non-revoked task returned error: %v", err)
	}
	if revoked {
		t.Fatal("IsRevoked should return false for a task that was never revoked")
	}
}

// testDelegationChain creates a task with a 3-link delegation chain and
// verifies GetDelegationChain returns all links in order.
func testDelegationChain(t *testing.T, s store.Store) {
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)

	chain := []store.DelegationLink{
		{
			Type:          "human",
			ID:            "human:alice@example.com",
			AuthorizedAt:  now.Unix() - 200,
			ScopeNarrowed: false,
		},
		{
			Type:          "task",
			ID:            "task:intermediate-001",
			AuthorizedAt:  now.Unix() - 100,
			ScopeNarrowed: false,
		},
		{
			Type:          "task",
			ID:            "task:leaf-001",
			AuthorizedAt:  now.Unix(),
			ScopeNarrowed: true,
		},
	}

	task := &store.TaskRecord{
		ID:              "task-delegation-001",
		Purpose:         "delegation chain test",
		Scopes:          []string{"github:repo:read"},
		Status:          store.TaskStatusActive,
		DelegationChain: chain,
		CreatedAt:       now,
		ExpiresAt:       now.Add(30 * time.Minute),
		Metadata:        map[string]string{},
	}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	got, err := s.GetDelegationChain(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetDelegationChain failed: %v", err)
	}

	if len(got) != len(chain) {
		t.Fatalf("DelegationChain length: want %d, got %d", len(chain), len(got))
	}

	for i, link := range chain {
		assertDelegationLink(t, i, link, got[i])
	}
}

// testAppendAndQueryEvents appends 3 events for the same task and queries
// them back, verifying all fields.
func testAppendAndQueryEvents(t *testing.T, s store.Store) {
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	taskID := "task-events-001"

	events := []*store.AuditEvent{
		{
			ID:        "evt-001",
			Timestamp: now,
			Event:     "task.created",
			TaskID:    taskID,
			DelegationChain: []store.DelegationLink{
				{Type: "human", ID: "human:bob@example.com", AuthorizedAt: now.Unix(), ScopeNarrowed: false},
			},
			Payload: map[string]any{"purpose": "test task"},
		},
		{
			ID:        "evt-002",
			Timestamp: now.Add(1 * time.Second),
			Event:     "task.status_changed",
			TaskID:    taskID,
			DelegationChain: []store.DelegationLink{
				{Type: "human", ID: "human:bob@example.com", AuthorizedAt: now.Unix(), ScopeNarrowed: false},
			},
			Payload: map[string]any{"old_status": "active", "new_status": "completed"},
		},
		{
			ID:        "evt-003",
			Timestamp: now.Add(2 * time.Second),
			Event:     "task.completed",
			TaskID:    taskID,
			DelegationChain: []store.DelegationLink{
				{Type: "human", ID: "human:bob@example.com", AuthorizedAt: now.Unix(), ScopeNarrowed: false},
			},
			Payload: map[string]any{"result": "success"},
		},
	}

	for _, evt := range events {
		if err := s.AppendEvent(ctx, evt); err != nil {
			t.Fatalf("AppendEvent(%s) failed: %v", evt.ID, err)
		}
	}

	got, err := s.QueryEvents(ctx, store.EventFilter{TaskID: taskID})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("QueryEvents: want 3 events, got %d", len(got))
	}

	for i, evt := range events {
		assertEqual(t, "Event["+evt.ID+"].ID", evt.ID, got[i].ID)
		assertEqual(t, "Event["+evt.ID+"].Event", evt.Event, got[i].Event)
		assertEqual(t, "Event["+evt.ID+"].TaskID", evt.TaskID, got[i].TaskID)
		if !got[i].Timestamp.Equal(evt.Timestamp) {
			t.Errorf("Event[%s].Timestamp: want %v, got %v", evt.ID, evt.Timestamp, got[i].Timestamp)
		}
		if len(got[i].DelegationChain) != len(evt.DelegationChain) {
			t.Errorf("Event[%s].DelegationChain length: want %d, got %d",
				evt.ID, len(evt.DelegationChain), len(got[i].DelegationChain))
		}
	}
}

// testQueryEventsFilters tests filtering by TaskID, time range, event type,
// and limit.
func testQueryEventsFilters(t *testing.T, s store.Store) {
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create events across two tasks and spread over time.
	// Use distinct event names (prefixed with "filter.") to avoid
	// collisions with events inserted by earlier tests that share
	// the same store instance.
	eventsData := []struct {
		id     string
		taskID string
		event  string
		offset time.Duration
	}{
		{"filter-evt-001", "task-filter-A", "filter.created", 0},
		{"filter-evt-002", "task-filter-A", "filter.status_changed", 10 * time.Second},
		{"filter-evt-003", "task-filter-B", "filter.created", 20 * time.Second},
		{"filter-evt-004", "task-filter-A", "filter.completed", 30 * time.Second},
		{"filter-evt-005", "task-filter-B", "filter.status_changed", 40 * time.Second},
	}

	for _, ed := range eventsData {
		evt := &store.AuditEvent{
			ID:              ed.id,
			Timestamp:       base.Add(ed.offset),
			Event:           ed.event,
			TaskID:          ed.taskID,
			DelegationChain: []store.DelegationLink{},
			Payload:         map[string]any{},
		}
		if err := s.AppendEvent(ctx, evt); err != nil {
			t.Fatalf("AppendEvent(%s) failed: %v", ed.id, err)
		}
	}

	// Filter by TaskID
	t.Run("ByTaskID", func(t *testing.T) {
		got, err := s.QueryEvents(ctx, store.EventFilter{TaskID: "task-filter-A"})
		if err != nil {
			t.Fatalf("QueryEvents failed: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("want 3 events for task-filter-A, got %d", len(got))
		}
	})

	// Filter by time range (Since/Until)
	t.Run("ByTimeRange", func(t *testing.T) {
		got, err := s.QueryEvents(ctx, store.EventFilter{
			Since: base.Add(5 * time.Second),
			Until: base.Add(25 * time.Second),
		})
		if err != nil {
			t.Fatalf("QueryEvents failed: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 events in time range, got %d", len(got))
		}
		assertEqual(t, "first event ID", "filter-evt-002", got[0].ID)
		assertEqual(t, "second event ID", "filter-evt-003", got[1].ID)
	})

	// Filter by Event type
	t.Run("ByEventType", func(t *testing.T) {
		got, err := s.QueryEvents(ctx, store.EventFilter{Event: "filter.created"})
		if err != nil {
			t.Fatalf("QueryEvents failed: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 filter.created events, got %d", len(got))
		}
	})

	// Filter by Limit
	t.Run("ByLimit", func(t *testing.T) {
		got, err := s.QueryEvents(ctx, store.EventFilter{Limit: 2})
		if err != nil {
			t.Fatalf("QueryEvents failed: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 events with limit, got %d", len(got))
		}
	})
}

func assertEqual(t *testing.T, field, want, got string) {
	t.Helper()
	if want != got {
		t.Errorf("%s: want %q, got %q", field, want, got)
	}
}

func assertDelegationLink(t *testing.T, index int, want, got store.DelegationLink) {
	t.Helper()
	if want.Type != got.Type {
		t.Errorf("DelegationChain[%d].Type: want %q, got %q", index, want.Type, got.Type)
	}
	if want.ID != got.ID {
		t.Errorf("DelegationChain[%d].ID: want %q, got %q", index, want.ID, got.ID)
	}
	if want.AuthorizedAt != got.AuthorizedAt {
		t.Errorf("DelegationChain[%d].AuthorizedAt: want %d, got %d", index, want.AuthorizedAt, got.AuthorizedAt)
	}
	if want.ScopeNarrowed != got.ScopeNarrowed {
		t.Errorf("DelegationChain[%d].ScopeNarrowed: want %v, got %v", index, want.ScopeNarrowed, got.ScopeNarrowed)
	}
}
