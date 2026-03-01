package audit

// Event type constants for all significant events in the system.
const (
	EventTaskCreated       = "task.created"
	EventTaskCompleted     = "task.completed"
	EventTaskFailed        = "task.failed"
	EventTaskExpired       = "task.expired"
	EventCredentialIssued  = "credential.issued"
	EventCredentialRevoked = "credential.revoked"
	EventToolInvoked       = "tool.invoked"
	EventScopeDenied       = "scope.denied"
	EventBundleResolved    = "scope.bundle_resolved"
)
