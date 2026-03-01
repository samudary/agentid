package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/samudary/agentid/pkg/store"
)

// BuildDelegationChain constructs the delegation chain for a new task.
// For root tasks (parentID starts with "human:"), creates a new chain with the human as first link.
// For sub-tasks, extends the parent's chain with a new link.
// scopeNarrowed indicates whether the child's scopes are narrower than the parent's.
// Delegation depth validation is performed by the caller before invoking this function.
func BuildDelegationChain(
	taskID string,
	parentID string,
	parentChain []store.DelegationLink,
	scopeNarrowed bool,
) ([]store.DelegationLink, error) {
	now := time.Now().Unix()

	if strings.HasPrefix(parentID, "human:") {
		return []store.DelegationLink{
			{
				Type:          "human",
				ID:            parentID,
				AuthorizedAt:  now,
				ScopeNarrowed: false,
			},
			{
				Type:          "task",
				ID:            taskID,
				AuthorizedAt:  now,
				ScopeNarrowed: scopeNarrowed,
			},
		}, nil
	}

	if len(parentChain) == 0 {
		return nil, fmt.Errorf("parent task %q has empty delegation chain", parentID)
	}

	chain := make([]store.DelegationLink, len(parentChain)+1)
	copy(chain, parentChain)
	chain[len(parentChain)] = store.DelegationLink{
		Type:          "task",
		ID:            taskID,
		AuthorizedAt:  now,
		ScopeNarrowed: scopeNarrowed,
	}

	return chain, nil
}
