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
func BuildDelegationChain(
	taskID string,
	parentID string,
	parentChain []store.DelegationLink,
	scopeNarrowed bool,
	maxDepth int,
) ([]store.DelegationLink, error) {
	now := time.Now().Unix()

	if strings.HasPrefix(parentID, "human:") {
		// Root task: chain starts with the human authorizer
		return []store.DelegationLink{
			{
				Type:          "human",
				ID:            parentID,
				AuthorizedAt:  now,
				ScopeNarrowed: false, // Human root is never "narrowed"
			},
			{
				Type:          "task",
				ID:            taskID,
				AuthorizedAt:  now,
				ScopeNarrowed: scopeNarrowed,
			},
		}, nil
	}

	// Sub-task: extend parent chain
	if len(parentChain) == 0 {
		return nil, fmt.Errorf("parent task %q has empty delegation chain", parentID)
	}

	// Check delegation depth: count existing task links (excluding human root)
	taskDepth := 0
	for _, link := range parentChain {
		if link.Type == "task" {
			taskDepth++
		}
	}
	if taskDepth >= maxDepth {
		return nil, fmt.Errorf("delegation depth exceeded: current depth %d, max %d", taskDepth, maxDepth)
	}

	// Build new chain = parent chain + this task
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
