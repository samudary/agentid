package sqlite

// schema contains the DDL statements executed on store initialization.
const schema = `
CREATE TABLE IF NOT EXISTS tasks (
	id              TEXT PRIMARY KEY,
	parent_id       TEXT NOT NULL DEFAULT '',
	purpose         TEXT NOT NULL DEFAULT '',
	scopes          TEXT NOT NULL DEFAULT '[]',
	status          TEXT NOT NULL DEFAULT 'active',
	delegation_chain TEXT NOT NULL DEFAULT '[]',
	metadata        TEXT NOT NULL DEFAULT '{}',
	created_at      TEXT NOT NULL,
	expires_at      TEXT NOT NULL,
	completed_at    TEXT,
	status_reason   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_expires_at ON tasks(expires_at);

CREATE TABLE IF NOT EXISTS revocations (
	task_id     TEXT PRIMARY KEY,
	revoked_at  TEXT NOT NULL,
	reason      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS audit_events (
	id               TEXT PRIMARY KEY,
	timestamp        TEXT NOT NULL,
	event            TEXT NOT NULL,
	task_id          TEXT NOT NULL,
	delegation_chain TEXT NOT NULL DEFAULT '[]',
	payload          TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_audit_events_task_id ON audit_events(task_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_timestamp ON audit_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_audit_events_event ON audit_events(event);
`
