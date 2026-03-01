# Differentiation Overview

- **Version**: 0.1.0
- **Date**: 2026-02-27
- **Status**: Draft

## Overview

This document provides an overview of how AgentID differs from existing approaches to agent authentication and authorization, and why a new system might be needed. Existing tools provide essential building blocks such as OAuth for token formats, JWTs for claims, OPA for policy evaluation, API gateways for request proxying. However, none of them model the concept of an agent task as a distinct identity lifecycle. AgentID fills that gap with purpose-built primitives for task-scoped credentials, recursive delegation chains, scope narrowing enforcement, and task-bound credential expiry.

## vs. OAuth 2.0 Token Exchange (RFC 8693)

OAuth 2.0 Token Exchange enables single-hop delegation where a Service A exchanges its token for a token representing Service B, acting on behalf of a user. The token format is standardized, widely supported, and integrates with existing identity providers. For service-to-service delegation where one service calls another, RFC 8693 is a well-understood, interoperable solution.

Agent workflows are structurally different from service-to-service delegation where a root planner spawns a sub-planner, which spawns three workers, each of which invokes multiple tools. That is a recursive delegation tree of arbitrary depth, and not the single-hop exchange typical with OAuth.

Agent workflows are unique in that they require:

- **Recursive delegation chains with depth tracking.** Token exchange handles one hop. Chaining multiple exchanges together is technically possible, but nothing in the specification tracks the full chain or enforces a maximum depth.
- **Per-hop scope narrowing enforcement.** There is no mechanism in the spec to guarantee that each successive exchange produces a token with equal or narrower permissions than its predecessor. The narrowing invariant must be implemented and enforced by application logic.
- **Task lifecycle binding.** OAuth tokens have expiry times, but their lifetime is not tied to the completion or failure of a specific unit of work. A credential born when a task starts and dying when it ends, regardless of clock time, is not a concept RFC 8693 provides.
- **Parent-child relationship as a first-class concept.** The delegation chain connecting a human authorizer through intermediate agent tasks to a leaf worker is not represented in the token. There is no standard claim for encoding this provenance.

You could chain multiple token exchanges together and build application-level logic to enforce narrowing, track chains, and bind credentials to task lifecycles. That application-level logic is what AgentID provides as a purpose-built system.

## vs. Service Accounts + RBAC

Service accounts with role-based access control provide simple, persistent identity. An "agent" service account assigned a "code-contributor" role works well for straightforward cases where a single agent performs a known set of operations. The model is widely understood, supported by every major cloud provider and identity platform, and easy to set up.

This model breaks down in multi-agent, multi-task environments:

- **Concurrent agent confusion.** When multiple agents operate under the same service account simultaneously, the audit trail cannot distinguish which agent performed which action. Every action is attributed to the shared account, making incident investigation and compliance reporting unreliable.
- **Static roles violate least privilege for sub-tasks.** A planner agent may need broad permissions, but its worker sub-tasks often need only a narrow subset. The service account's static role grants the full set of permissions to every task that uses the account, including tasks that should only read, not write.
- **No automatic credential expiry tied to task lifecycle.** Service accounts persist indefinitely. When an agent task completes, the credentials remain valid with their full permissions. There is no built-in mechanism to revoke access when the work is done.
- **No delegation provenance.** Service accounts do not carry information about who authorized the work or how permissions were delegated. Tracing an action back through a delegation tree to the human who authorized it requires external record-keeping that the identity system does not enforce.

Task identity addresses these gaps by making credentials ephemeral (born at task start, invalidated at task end), scoped to the specific permissions the task needs, and embedded with the full delegation chain from human authorizer through every intermediate task.

## vs. API Gateway + Open Policy Agent (OPA)

An API gateway combined with OPA provides request-level authorization, i.e. "Is this token allowed to call this endpoint?" This is a necessary layer in any production system. Gateways handle routing, rate limiting, and TLS termination. OPA evaluates policies against structured input and returns allow/deny decisions. Together, they are the standard approach to API access control.

What this combination does not provide is the agent-specific identity layer above request-level policy:

- **Task lifecycle management.** Gateways and OPA have no concept of a credential being born when a task starts and dying when it ends. Tokens are issued and managed by an external identity provider; the gateway validates them per-request but does not participate in their lifecycle.
- **Delegation chain tracking.** OPA evaluates individual requests in isolation. It does not know that a given request is part of a task that was spawned by another task that was authorized by a specific human. Cross-request provenance is outside its evaluation model.
- **Scope narrowing enforcement across delegation depth.** OPA can verify "does this token carry scope X?" but not "did the parent task that issued this token actually hold scope X, and did its parent hold a superset?" Enforcing the narrowing invariant across a delegation tree requires state and semantics that request-level policy evaluation does not maintain.
- **MCP-native tool discovery.** API gateways proxy HTTP requests. They do not expose tool capabilities as MCP definitions that agents can programmatically discover and reason about before invocation.
- **Agent-specific telemetry.** Gateway logs capture HTTP requests such as method, path, status code, latency. They do not capture task trees, delegation chains, cost-per-task attribution, or multi-tool workflow state.

The gateway + OPA pattern is the right enforcement point for request-level policy, and AgentID is designed to work with it rather than replace it. A future implementation of AgentID integrates directly with OPA for complex policy evaluation with an architectural distinction where the gateway and OPA enforce policy at the request boundary, while AgentID provides the agent-specific identity model, delegation semantics, and task lifecycle that sit above that boundary and give the policy layer richer context to evaluate against.

## Summary Comparison

| Capability | OAuth Token Exchange | Service Accounts + RBAC | API Gateway + OPA | AgentID |
|---|---|---|---|---|
| Ephemeral task credentials | No | No | No | Yes |
| Recursive delegation chains | No | No | No | Yes |
| Scope narrowing enforcement | No | No | No | Yes |
| Task lifecycle binding | No | No | No | Yes |
| Delegation chain audit | No | No | No | Yes |
| MCP tool discovery | No | No | No | Yes |
| Request-level policy | Yes | Yes | Yes | Yes (To be implemented in a future version via OPA) |
| Industry standard | Yes | Yes | Yes | Emerging |

Existing tools provide the building blocks with OAuth for token formats, JWTs for claims, OPA for policy evaluation, API gateways for request proxying. What is missing is the semantic layer that understands agent tasks as a distinct identity lifecycle with recursive delegation, automatic scope narrowing, task-bound credential expiry, and full-chain auditability from any leaf action back to the human who authorized it. That semantic layer is AgentID.
