# AgentID

Task-scoped identity and auth for autonomous AI agents.

## The Problem

AI agents today borrow human credentials, use static API keys shared across concurrent tasks, and produce audit trails that cannot distinguish which agent did what or who authorized it. When a planner agent spawns sub-tasks, each inherits the parent's full permissions rather than a minimal subset. There is no standard mechanism for ephemeral, scoped, auditable credentials that trace delegation from a human authorizer through every intermediate agent to the leaf action.

## How AgentID Works

AgentID introduces task identity JWTs via short-lived, scoped credentials issued per unit of agent work. A human (or parent task) creates a task credential with specific permissions expressed as `tool:resource:action` scopes. The agent uses this JWT to invoke tools through an MCP proxy that validates the token, checks scopes, translates credentials for upstream APIs, and records every action in an audit trail. Sub-tasks receive narrower scopes than their parent, enforcing least privilege through the entire delegation tree.

### Install

```bash
go install github.com/samudary/agentid/cmd/agentid@latest

# Or from a local source
go install ./cmd/agentid
```

### Configure

```
cp configs/example-gateway.yaml gateway.yaml
export GITHUB_TOKEN=ghp_your_token_here
```

### Start the server

```
agentid serve --config gateway.yaml
```

### Create a task credential

```
agentid task create \
  --authorizer "human:you@company.com" \
  --bundle code-contributor \
  --purpose "implement feature" \
  --ttl 30m \
  --config gateway.yaml
```

### Use it with the MCP proxy

```bash
# List available tools
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'

# Call a tool
curl -X POST http://localhost:8080/mcp \
  -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"github_get_file","arguments":{"owner":"samudary","repo":"agentid","path":"README.md"}},"id":2}'
```

### Check the audit trail

```
agentid audit tail --config gateway.yaml
```

### Revoke the credential

```
agentid task revoke <task-id> --reason "done" --config gateway.yaml
```

## Core Concepts

### Task Identity

Every agent task receives its own JWT credential with a unique UUIDv7 identifier, explicit scopes, and a bounded lifetime. Credentials are born when a task starts and die when it ends or expires. There are no long-lived tokens. If more time is needed, the parent creates a new sub-task.

### Scope Narrowing

Permissions use the format `tool:resource:action` (e.g., `github:repo:read`). When a parent task delegates to a child, the child's scopes must be equal to or narrower than the parent's. A wildcard segment (`*`) can be replaced with a specific value, but a specific value can never be replaced with a wildcard. This enforces least privilege through the entire delegation tree.

### Delegation Chains

Every JWT carries the full authorization provenance from the human who authorized the work through every intermediate task to the current task. This chain is embedded in the token itself, so any action can be traced back to its human authorizer without external lookups. Each link records whether scopes were narrowed at that delegation step.

### Scope Bundles

Named groups of scopes defined in gateway configuration (e.g., `code-contributor` bundles `github:repo:read`, `github:repo:write`, `github:pulls:write`, `github:actions:read`). Bundles prevent scope fatigue and helps mitigate the tendency to guess at permission arrays or grant overly broad wildcards. Bundles are expanded to granular scopes before JWT encoding; they are a configuration convenience, not a runtime concept.

### MCP Proxy

The proxy validates task JWTs, checks scope authorization, translates credentials for upstream APIs (agents never see upstream tokens like GitHub PATs), and logs every action. It exposes tools via JSON-RPC 2.0 (the MCP protocol), enabling agents to discover available tools and invoke them programmatically.

## Architecture

1. Agent / CLI (Task JWT)
2. --- MCP (JSON-RPC 2.0)
3. ------> AgentID Proxy (JWT validation, scope check, auth translate, audit log)
4. --- Upstream API
5. ------> Github, etc.

## Specs

The proposed identity model and scope semantics are documented:

- [Task Identity Claims](spec/task-identity-claims.md) - JWT claim structure, signing, lifetime rules, and validation order
- [Scope Conventions](spec/scope-conventions.md) - Scope format, wildcard semantics, narrowing rules, and bundles
- [Differentiation](spec/differentiation.md) - How AgentID differs from OAuth token exchange, service accounts, and API gateways

## Using with MCP Clients

AgentID speaks standard MCP over JSON-RPC 2.0 which means any MCP client can talk to it. The only AgentID-specific part is passing the task JWT as a Bearer token.

```go
import (
    "github.com/mark3labs/mcp-go/client"
    "github.com/mark3labs/mcp-go/client/transport"
    "github.com/mark3labs/mcp-go/mcp"
)

mcpClient, _ := client.NewStreamableHttpClient(
    "http://localhost:8080/mcp",
    transport.WithHTTPHeaders(map[string]string{
        "Authorization": "Bearer " + taskJWT,
    }),
)
defer mcpClient.Close()

mcpClient.Start(ctx)

tools, _ := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
result, _ := mcpClient.CallTool(ctx, mcp.CallToolRequest{
    Params: mcp.CallToolParams{
        Name: "github_get_file",
        Arguments: map[string]any{
            "owner": "samudary",
            "repo":  "agentid",
            "path":  "README.md",
        },
    },
})
```

See [`examples/basic-github/main.go`](examples/basic-github/main.go) for a complete working example.

## CLI Reference

```
agentid serve --config gateway.yaml
agentid task create --authorizer <id> --scopes <scopes> --ttl <duration> --config gateway.yaml
agentid task create --authorizer <id> --bundle <name> --ttl <duration> --config gateway.yaml
agentid task inspect <task-id> --config gateway.yaml
agentid task revoke <task-id> --reason <reason> --config gateway.yaml
agentid scopes list-bundles --config gateway.yaml
agentid scopes expand-bundle <name> --config gateway.yaml
agentid audit query --task <id> --since <duration> --config gateway.yaml
agentid audit tail --config gateway.yaml
```

## License

Apache 2.0
