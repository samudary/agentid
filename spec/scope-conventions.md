# Scope Conventions Specification

**Version**: 0.1.0
**Date**: 2026-02-27
**Status**: Draft

## Overview

Scopes are the permission primitives of the AgentID system. Each scope is a structured string that encodes a specific permission grant over a particular tool, resource, and action. Scopes are carried in task identity JWTs (see `task-identity-claims.md`) and evaluated at the gateway proxy layer to determine whether a task is authorized to perform a requested operation. The scope system enforces a strict narrowing invariant, i.e. when a parent task delegates to a child task, the child's scopes must be equal to or narrower than the parent's; never broader. This guarantees that delegated authority can only decrease through the task tree, preserving least-privilege from the human authorizer down to every leaf task.

## Scope Format

A scope is a string composed of exactly three colon-separated segments:

```
tool:resource:action
```

- **tool** - The external service or integration (e.g., `github`, `launchdarkly`, `datadog`).
- **resource** - The resource type within the tool (e.g., `repo`, `flags`, `pulls`).
- **action** - The operation being performed on the resource (e.g., `read`, `write`, `create`).

### Segment Rules

Each segment must be one of:

- A non-empty string matching the regex pattern `[a-z0-9-]+` (lowercase alphanumeric characters and hyphens), or
- The wildcard character `*`

A valid scope matches the following composite pattern:

```
^([a-z0-9-]+|\*):([a-z0-9-]+|\*):([a-z0-9-]+|\*)$
```

### Valid Examples

| Scope                | Meaning |
|----------------------|---------|
| `github:repo:read`   | Read access to GitHub repositories |
| `github:repo:write`  | Write access to GitHub repositories |
| `github:repo:*`      | All actions on GitHub repositories |
| `github:*:*`         | All actions on all GitHub resources |
| `*:*:*`              | Unrestricted (root scope, rarely granted) |
| `launchdarkly:flags:create` | Create feature flags in LaunchDarkly |

### Invalid Examples

| Input              | Reason                                                                 |
| ------------------ | ---------------------------------------------------------------------- |
| `github:repo`      | Missing segment. Scopes require exactly three colon-separated segments |
| `github::read`     | The resource segment between the two colons is empty                   |
| `GitHub:Repo:Read` | Segments must be lowercase                                             |
| *(empty string)*   | Scopes must be non-empty                                               |
| `a:b:c:d`          | Too many segments                                                      |
| `git*:repo:read`   | Partial wildcard. `*` must be the entire segment, not part of it       |

## Wildcard Semantics

The wildcard `*` operates exclusively at the segment level. A asterisk in a segment means "all values for this segment." There is no partial wildcard matching; the wildcard must be the entire content of a segment.

- `github:repo:*` grants all actions on GitHub repositories.
- `github:*:*` grants all actions on all GitHub resource types.
- `*:*:*` grants all actions on all resources across all tools.

A segment value of `git*` or `*hub` is invalid. The wildcard character has no meaning when combined with other characters. The scope `git*:repo:read` does not match `github:repo:read` and is interpreted as a malformed scope and must be rejected at validation time.

## Formal Narrowing Rules

Scope narrowing is the mechanism that enforces the delegation invariant: child tasks can never hold broader permissions than their parent.

### Definition

A child scope `C` is a **valid narrowing** of a parent scope `P` if and only if, for every segment position `i` (where `i` ranges over tool, resource, and action), either the child's segment is identical to the parent's segment, or the parent's segment is the wildcard `*`.

### Pseudocode

```
narrows(child, parent):
    for each segment in [tool, resource, action]:
        if child[segment] != parent[segment] AND parent[segment] != "*":
            return false
    return true
```

### Key Principle

A scope with `*` at a given segment is strictly broader than any scope with a specific value at that same position. Narrowing can only replace wildcards with specific values, never the reverse. Replacing a specific value with `*` is a widening operation and is always rejected.

### Narrowing Truth Table

| Parent Scope | Requested Child Scope | Allowed? | Reason |
|---|---|---|---|
| `github:repo:*` | `github:repo:read` | Yes | Action narrowed from wildcard |
| `github:repo:read` | `github:repo:write` | No | Action widened |
| `github:*:*` | `github:repo:write` | Yes | Resource and action narrowed |
| `github:repo:read` | `launchdarkly:flags:read` | No | Different tool |
| `*:*:*` | `github:repo:read` | Yes | All segments narrowed from root |
| `*:*:*` | `*:*:read` | Yes | Action narrowed, others kept wild |
| `github:repo:read` | `github:repo:read` | Yes | Identical is valid |
| `github:repo:*` | `github:*:read` | No | Resource widened from specific to wildcard |

## Multi-Scope Validation

Tasks typically carry multiple scopes. When a child task requests a set of scopes from a parent task, validation operates as follows.

For a child requesting scopes `[C1, C2, ...]` from a parent holding scopes `[P1, P2, ...]`, **each** child scope must have **at least one** parent scope that covers it through narrowing.

```
valid(child_scopes, parent_scopes):
    for each child_scope in child_scopes:
        covered = false
        for each parent_scope in parent_scopes:
            if narrows(child_scope, parent_scope):
                covered = true
                break
        if not covered:
            return false
    return true
```

The parent does not need to hold the exact same set of scopes as the child. The parent's scope set must be a **superset** that covers every requested child scope through the narrowing relation.

### Examples

**Allowed**: Parent holds `["github:repo:*", "launchdarkly:flags:read"]`. Child requests `["github:repo:read"]`. The child's single scope narrows from `github:repo:*`.

**Allowed**: Parent holds `["*:*:*"]`. Child requests `["github:repo:read", "launchdarkly:flags:create"]`. Both child scopes narrow from `*:*:*`.

**Denied**: Parent holds `["github:repo:read"]`. Child requests `["github:repo:read", "launchdarkly:flags:create"]`. The second child scope has no covering parent scope.

## Scope Bundles

Scope bundles are named, reusable groups of scopes defined in gateway configuration. Bundles provide curated permission sets for common workflows with the hope of preventing scope fatigue. The goal is to avoid or make it less likely for developers to guess at granular permission arrays or grant overly broad wildcards out of frustration.

### Format

Bundles are defined as a YAML map under the `scope_bundles` key in a gateway configuration file.

Each bundle has:

| Field         | Type             | Required | Description |
|---------------|------------------|----------|-------------|
| `description` | string           | Yes      | Human-readable description of the bundle's purpose |
| `scopes`      | array of strings | No       | List of granular scope strings in `tool:resource:action` format |
| `includes`    | array of strings | No       | List of other bundle names to include (recursive composition) |

A bundle must have at least one of `scopes` or `includes`. A bundle with neither is a configuration validation error.

### Example

```yaml
scope_bundles:
  code-contributor:
    description: "Read and write code, manage PRs, view CI status"
    scopes:
      - github:repo:read
      - github:repo:write
      - github:pulls:write
      - github:actions:read

  feature-flag-manager:
    description: "Create and manage feature flags"
    scopes:
      - launchdarkly:flags:create
      - launchdarkly:flags:read
      - launchdarkly:flags:write

  standard-feature-work:
    description: "Typical end-to-end feature development"
    includes:
      - code-contributor
      - feature-flag-manager
```

### Bundle Expansion

When a task is created using a bundle reference, the gateway expands the bundle to its constituent granular scopes before JWT encoding. The expansion process is:

1. **Resolve `includes` first** - Recursively expand all included bundles using depth-first traversal. If bundle `A` includes bundle `B`, and bundle `B` includes bundle `C`, then `C` is expanded first, then `B` (merging `C`'s scopes), then `A` (merging `B`'s scopes).

2. **Merge `scopes`** - After all includes are resolved, merge the bundle's own `scopes` list with the scopes collected from included bundles.

3. **Deduplicate** - Remove duplicate scope strings from the final list. The order of the resulting list is not significant.

4. **Validate** - After expansion, every scope in the final list is validated against the scope format defined in this specification. Invalid scopes are a configuration error.

### Circular Include Detection

Circular includes (e.g., bundle `A` includes `B`, bundle `B` includes `A`) are a configuration validation error. The gateway MUST detect circular includes at startup and refuse to start with an error message identifying the cycle. Detection occurs during the depth-first expansion: if a bundle is encountered that is already in the current expansion stack, a cycle exists.

### Runtime Behavior

JWTs always contain granular scopes and bundles are a configuration convenience, not a runtime concept. Scope narrowing validation, audit logging, and policy enforcement all operate on the resolved granular scopes. The bundle name is not encoded in the JWT. The audit log may record which bundle was used at task creation time as an informational event (see `scope.bundle_resolved` event type), but this is not used for authorization decisions.

---

## Reserved Scope Prefixes

The tool segment `agentid` is reserved for internal gateway operations. Scopes beginning with `agentid:` are used by the gateway itself for administrative functions.

Examples of reserved scopes:

| Scope                  | Purpose |
|------------------------|---------|
| `agentid:task:create`  | Permission to create new task identities |
| `agentid:task:revoke`  | Permission to revoke existing task identities |

Third-party tools MUST NOT use `agentid` as their tool segment. The gateway MUST reject tool adapter configurations that define operations under the `agentid` tool namespace.

---

## Cross-References

- **JWT claim structure**: The `scopes` claim that carries scope values in task identity tokens is defined in `task-identity-claims.md`.
- **Scope narrowing in delegation**: The delegation chain and `scope_narrowed` flag that track whether narrowing occurred at each delegation step are defined in the DelegationLink schema in `task-identity-claims.md`.
