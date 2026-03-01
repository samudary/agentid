# Task Identity Claims Specification

**Version**: 0.1.0
**Date**: 2026-02-27
**Status**: Draft

## Overview

A task identity JWT is a short-lived, signed credential that represents a single unit of autonomous agent work. It encodes the task's unique identity, its authorized permissions, the full delegation chain tracing authorization back to a human, and constraints governing sub-task creation. Task identity JWTs are the foundational primitive of the AgentID system where every agent action flows through a JWT that is scoped to the minimum necessary permissions, bound to the task's lifetime, and auditable through its embedded delegation provenance.

## Standard JWT Claims

| Claim | Type    | Required | Description                                                                                                                                                                                                                                           |
| ----- | ------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `iss` | string  | Yes      | Always the literal string `"agentid"`. This is a fixed value, not an instance identifier.                                                                                                                                                             |
| `sub` | string  | Yes      | The task's unique identity. Format: `"task:<uuidv7>"` where `<uuidv7>` is a lowercase UUIDv7 (RFC 9562) value. UUIDs MUST be lowercase per RFC 9562 Section 6.8. Example: `"task:01961c88-a3b7-7de1-bd35-4f22e384a080"`.                              |
| `iat` | integer | Yes      | Unix timestamp (seconds since epoch) indicating when the JWT was issued.                                                                                                                                                                              |
| `exp` | integer | Yes      | Unix timestamp (seconds since epoch) indicating when the JWT expires. Must be greater than `iat`.                                                                                                                                                     |
| `jti` | string  | Yes      | The JWT ID. Set to the same UUIDv7 value used in the `sub` claim, without the `task:` prefix. Provides standard JWT ID compliance and enables correlation between the JWT and stored task records. Example: `"01961c88-a3b7-7de1-bd35-4f22e384a080"`. |

## Custom Claims

| Claim                  | Type                    | Required | Description                                                                                                                                                                                                                                                                                                                                              |
| ---------------------- | ----------------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `purpose`              | string                  | Yes      | Human-readable description of why this task exists. Informational only. NOT used for authorization decisions. Exists for audit trail readability so humans reviewing logs can understand a task's intent without parsing scope arrays.                                                                                                                   |
| `scopes`               | array of strings        | Yes      | Granular permissions in `tool:resource:action` format. Always contains resolved granular scopes; scope bundles are expanded before JWT encoding. See `scope-conventions.md` for format details, wildcard semantics, and narrowing rules.                                                                                                                 |
| `delegation_chain`     | array of DelegationLink | Yes      | The full authorization provenance, ordered root-first (index 0 is the human authorizer or the original root). Encodes every delegation from the human authorizer through each intermediate task to the current task.                                                                                                                                     |
| `policy_context`       | object                  | No       | Arbitrary key-value string pairs passed through to policy evaluation. Not interpreted by the identity service itself. Enables organization-specific policy decisions without changes to the core claim schema. Example: `{"team": "platform", "project": "payments-v2", "sensitivity": "standard"}`.                                                     |
| `max_delegation_depth` | integer                 | Yes      | Maximum number of additional delegation levels this task can create. Decremented by 1 for each child task. When 0, this task cannot create sub-tasks. Example: a task with `max_delegation_depth: 3` can create a child with depth 2, which can create a grandchild with depth 1, which can create one final level with depth 0 (no further delegation). |
| `max_ttl_seconds`      | integer                 | Yes      | Maximum TTL in seconds that this task can grant to child tasks. A child task's TTL also cannot exceed this task's remaining lifetime at the moment of child creation.                                                                                                                                                                                    |
## DelegationLink Schema

Each element in the `delegation_chain` array is a DelegationLink object with the following fields. The last entry in the chain MUST have an `id` matching the JWT's `sub` claim (it represents the current task).

| Field            | Type    | Required | Description                                                                                                                                                                    |
| ---------------- | ------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `type`           | string  | Yes      | The type of entity in the chain. Either `"human"` (for a human authorizer) or `"task"` (for an agent task).                                                                    |
| `id`             | string  | Yes      | The entity's identifier. For humans: `"human:<email>"` (e.g., `"human:jane@company.com"`). For tasks: `"task:<uuidv7>"` (e.g., `"task:01961c88-a3b7-7de1-bd35-4f22e384a080"`). |
| `authorized_at`  | integer | Yes      | Unix timestamp (seconds since epoch) indicating when this entity authorized the delegation.                                                                                    |
| `scope_narrowed` | boolean | Yes      | Whether the scope set was narrowed compared to the parent link. Always `false` for the first link in the chain (the human root), since there is no parent to narrow from.      |

## Example JWT Payload

The following is a complete, valid JWT payload conforming to this specification:

```json
{
  "iss": "agentid",
  "sub": "task:01961c88-a3b7-7de1-bd35-4f22e384a080",
  "iat": 1740000000,
  "exp": 1740001800,
  "jti": "01961c88-a3b7-7de1-bd35-4f22e384a080",
  "purpose": "implement-feature-flag-for-dark-launch",
  "scopes": [
    "github:repo:write",
    "launchdarkly:flags:create"
  ],
  "delegation_chain": [
    {
      "type": "human",
      "id": "human:jane@company.com",
      "authorized_at": 1739999900,
      "scope_narrowed": false
    },
    {
      "type": "task",
      "id": "task:01961c80-beef-7000-a000-000000000001",
      "authorized_at": 1739999950,
      "scope_narrowed": false
    },
    {
      "type": "task",
      "id": "task:01961c88-a3b7-7de1-bd35-4f22e384a080",
      "authorized_at": 1740000000,
      "scope_narrowed": true
    }
  ],
  "policy_context": {
    "team": "platform",
    "project": "payments-v2",
    "sensitivity": "standard"
  },
  "max_delegation_depth": 3,
  "max_ttl_seconds": 1800
}
```

In this example:
- The human `jane@company.com` authorized the root task at timestamp `1739999900`.
- The root task (`01961c80-beef-7000-a000-000000000001`) delegated to the current task without narrowing scopes.
- The current task (`01961c88-a3b7-7de1-bd35-4f22e384a080`) was created with a narrowed scope set compared to its parent.
- The task can create up to 3 additional levels of sub-tasks, each of which can have a TTL of at most 1800 seconds (but also limited by this task's remaining lifetime).

---

## Signing

| Property         | Value                                                                                              |
| ---------------- | -------------------------------------------------------------------------------------------------- |
| Algorithm        | ES256 (ECDSA with NIST P-256 curve, SHA-256 hash)                                                  |
| Key generation   | ECDSA P-256 key pair, generated at first `agentid serve` startup if no key file is configured      |
| Key distribution | Public key served at `GET /.well-known/jwks.json` endpoint in JWK format for external verification |
| Key rotation     | TBD. Single key pair per gateway instance.                                                         |

The JWT header for all task identity tokens is:

```json
{
  "alg": "ES256",
  "typ": "JWT"
}
```

---

## Lifetime Rules

1. **Maximum TTL**: The maximum TTL for any task is configurable per gateway instance. The default is 60 minutes.

2. **Parent TTL constraint**: A task's TTL cannot exceed its parent's remaining TTL at the time of creation. If a parent has 15 minutes remaining, a child task's TTL is capped at 15 minutes regardless of the requested value.

3. **Child TTL constraint**: A task's `max_ttl_seconds` value cannot exceed its own remaining TTL. This ensures a task cannot grant children more time than it has left.

4. **Expiry enforcement**: Expired JWTs (where the current time exceeds the `exp` claim) are rejected immediately during validation. This follows standard JWT `exp` semantics.

5. **Revocation enforcement**: Revoked JWTs are rejected during validation. Revocation status is checked against the store's revocation index via a point lookup.

6. **No refresh mechanism**: Tasks do not extend their own lifetime. If more time is needed, the parent (or human) creates a new sub-task with a fresh TTL. This keeps the credential lifecycle simple and auditable where every credential has a single, immutable expiry.

---

## Validation Order

Validators must check claims in the following order. If any step fails, validation halts and the request is rejected with an appropriate error.

1. **Signature verification** — Verify the JWT signature using the ES256 public key. Reject if the signature is invalid or the algorithm does not match.

2. **Expiry check** — Verify that the current time is before the `exp` claim value. Reject if the token has expired.

3. **Issuer check** — Verify that the `iss` claim is `"agentid"`. Reject if the issuer does not match.

4. **Revocation check** — Look up the task ID (from the `sub` claim) in the store's revocation index. Reject if the task has been revoked.

5. **Scope check** — Verify that the task's `scopes` claim includes a scope that permits the requested operation. This check happens per-request at proxy time and is not part of JWT structural validation itself. It is documented here for completeness, as it is the final gate before a proxied request proceeds.

---

## Edge Cases

### Empty `delegation_chain`

An empty delegation chain is Invalid. Every task identity JWT must have at least one entry in the `delegation_chain` array. A root task created by a human has a single link of type `"human"`. A delegated task has the full chain from the human root through every intermediate task. A JWT with an empty `delegation_chain` must be rejected during issuance and validation.

### `max_delegation_depth` of 0

In this case the task cannot create sub-tasks. Any attempt to create a child task using this task as the parent must return an error indicating that the delegation depth has been exhausted. The task itself remains valid for tool invocations within its scopes.

### `scopes` is an empty array

A task with an empty `scopes` array is valid and can authenticate (provided the JWT is structurally valid) but cannot invoke any tools through the proxy, since every tool invocation requires at least one matching scope. This may be useful for tasks that exist solely to coordinate sub-tasks that carry the actual permissions.

### `purpose` is an empty string

The identity service should log a warning when issuing a JWT with an empty `purpose` string. This is valid but discouraged. The JWT is not rejected, since `purpose` is informational and not used for authorization. An empty purpose reduces audit trail readability.
