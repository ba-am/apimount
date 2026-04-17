# apimount

**A universal OpenAPI adapter.** Point it at any OpenAPI 3.0 / 3.1 spec and call every operation through whichever surface you need — CLI, MCP server, WebDAV, or NFS. All surfaces share one execution core that handles auth, retries, rate-limits, pagination, schema validation, and audit.

```bash
# CLI — zero setup, works everywhere
apimount --spec ./petstore.yaml --base-url $URL get /pet/42
apimount --spec ./petstore.yaml --base-url $URL post /pet --body '{"name":"Rex","photoUrls":[]}'
```

> **Status.** Phases 1–5 are shipped (core refactor, CLI UX, enterprise auth, reliability middlewares, MCP/WebDAV/NFS frontends). Phase 6 (observability & audit) is next — see [apimount_spec.md §14](apimount_spec.md#L734-L768) for the implementation order.

---

## Why apimount

Every API consumer re-writes the same plumbing: auth, retries, pagination, rate-limit backoff, request validation, audit logs. apimount turns a spec into a runnable surface so you get all of it for free, through the interface that fits your workflow:

| Surface | Use case | Status |
|---|---|---|
| **CLI** | Scripting, CI, humans in a terminal — zero setup, no mount | ✅ shipped |
| **MCP server** | Expose an API as first-class tools to Claude Code / Desktop / Cursor / any MCP client | ✅ shipped |
| **WebDAV server** | Browse an API from Finder / Explorer / any HTTP GUI, no kernel driver | ✅ shipped |
| **NFS server** | Cross-platform mount (Linux / macOS / Windows) with no kernel driver, runs in containers | ✅ shipped |
| **Library** | `pkg/apimount` importable Go module — reuse the parser, planner, executor | 🗺️ planned |

See [apimount_spec.md §1](apimount_spec.md#L28-L57) for the full architecture and [§2](apimount_spec.md#L61-L99) for the enterprise feature list (OAuth2, SigV4, Vault / Keychain / 1Password secrets, OTel traces + metrics, RBAC, audit log, Sigstore-signed releases).

---

## Installation

### Build from source

```bash
git clone https://github.com/ba-am/apimount
cd apimount
make build          # → bin/apimount
make install        # installs to $GOPATH/bin
```

---

## CLI

The CLI is the primary surface. No mount, no server, no kernel driver — just resolve an operation and run it.

### One-shot HTTP calls

```bash
apimount --spec ./petstore.yaml --base-url $URL get /pet/42
apimount --spec ./petstore.yaml --base-url $URL post /pet --body '{"name":"Rex","photoUrls":[]}'
apimount --spec ./petstore.yaml --base-url $URL delete /pet/42
apimount --spec ./petstore.yaml --base-url $URL call GET /pet/findByStatus --query status=available
```

`get` / `post` / `put` / `patch` / `delete` are aliases for `call METHOD PATH`. The concrete request path is matched against the spec's operations; literal segments win over `{param}` segments on ties.

**Flags:** `--query key=val` (repeatable), `--header key=val` (repeatable), `--body '<raw>'`, `--body-file path` (use `-` for stdin).

### Explore a spec

```bash
apimount tree     --spec ./petstore.yaml --group-by path   # print the virtual tree
apimount validate --spec https://petstore3.swagger.io/api/v3/openapi.json
apimount spec diff old.yaml new.yaml                       # exits non-zero on breaking changes
```

### Profiles

Save reusable spec + base URL + auth bundles in `~/.apimount.yaml`:

```yaml
profiles:
  petstore:
    spec: https://petstore3.swagger.io/api/v3/openapi.json
    base-url: https://petstore3.swagger.io/api/v3
  github:
    spec: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.yaml
    base-url: https://api.github.com
    auth-bearer: ghp_xxxxxxxxxxxx
```

```bash
apimount profile list
apimount profile show github     # auth values redacted
apimount --profile github get /repos/torvalds/linux
```

### Diagnostics

```bash
apimount doctor
```

Reports OS / arch, Go runtime, spec reachability, and config file presence.

---

## Server frontends

### MCP — expose every operation as a tool for Claude / agents

```bash
# stdio (default) — add to Claude Code / Desktop
apimount serve mcp --spec ./petstore.yaml --base-url $URL

# SSE — remote deployment
apimount serve mcp --spec ./petstore.yaml --base-url $URL --transport sse --addr :8080
```

Each OpenAPI operation becomes one MCP tool. Tool name = `operationId`; parameters are derived from the spec's path/query/header params and request body schema. Auth, retry, rate-limiting, pagination, and validation are all inherited from the execution core.

### WebDAV — browse from Finder / Explorer

```bash
apimount serve webdav --spec ./petstore.yaml --base-url $URL --addr :8080
```

Connect from macOS Finder (`Connect to Server → http://localhost:8080`), Windows Explorer (`Map Network Drive`), or any WebDAV client. The API tree is read-only.

### NFS — cross-platform mount

```bash
apimount serve nfs --spec ./petstore.yaml --base-url $URL --addr :2049

# Then mount from any OS:
# Linux:   sudo mount -t nfs -o vers=3,nolock,tcp,port=2049 127.0.0.1:/ /mnt/api
# macOS:   sudo mount -t nfs -o vers=3,resvport,nolock 127.0.0.1:/ /mnt/api
# Windows: mount -o nolock 127.0.0.1:/ Z:
```

No kernel driver needed — serves NFSv3 over TCP. The API tree is read-only.

---

## Auth

### Static credentials

```bash
apimount --auth-bearer ghp_xxxx ...
apimount --auth-basic user:password ...
apimount --auth-apikey mykey --auth-apikey-param X-Custom-Key ...
```

### OAuth2 client credentials (machine-to-machine)

```bash
apimount \
  --auth-oauth2-client-id myapp \
  --auth-oauth2-client-secret 'env:APP_SECRET' \
  --auth-oauth2-token-url https://issuer.example.com/oauth/token \
  --auth-oauth2-scopes read:pets,write:pets \
  --spec ./petstore.yaml --base-url $URL \
  get /pet/42
```

apimount exchanges the client ID/secret for an access token on first use and caches it until ~1 minute before expiry, refreshing automatically. No token touches disk.

### OAuth2 device-code flow (interactive login)

```bash
# One-time login — opens a URL in your browser
apimount auth login \
  --auth-oauth2-client-id myapp \
  --auth-oauth2-token-url https://issuer.example.com/oauth/token \
  --auth-oauth2-device-url https://issuer.example.com/oauth/device/code \
  --profile github

# Subsequent calls reuse the cached token (auto-refreshes if needed)
apimount --profile github get /repos/torvalds/linux

# Check token status or logout
apimount auth status --profile github
apimount auth logout --profile github
```

Tokens are cached to `~/.apimount/tokens/` (chmod 0600) and auto-refresh using the stored refresh token.

### mTLS (mutual TLS)

```bash
apimount --auth-mtls-cert ./client.crt --auth-mtls-key ./client.key \
  --auth-mtls-ca ./ca.crt --spec ./internal-api.yaml get /status
```

### AWS SigV4

```bash
apimount \
  --auth-sigv4-access-key 'env:AWS_ACCESS_KEY_ID' \
  --auth-sigv4-secret-key 'env:AWS_SECRET_ACCESS_KEY' \
  --auth-sigv4-region us-east-1 \
  --spec ./api-gateway.yaml get /items
```

### Secret references

Any credential flag accepts an indirection instead of a literal value, so secrets never land in shell history or profile YAML:

| Ref | Resolves to |
|---|---|
| `env:VAR_NAME` | `os.Getenv("VAR_NAME")` — errors if unset |
| `file:/path/to/secret` | file contents (trailing newline trimmed); file must be chmod `0600` on Unix |
| `op:op://vault/item/field` | 1Password CLI (`op read`) |
| `keychain:service/account` | macOS Keychain (`security find-generic-password`) |
| `literal:value` | the literal string `value` (opt-in, for completeness) |
| anything else | treated as a literal (backwards compatible) |

```bash
# Read token from 1Password
apimount --auth-bearer 'op:op://Private/github-token/credential' ...

# Read from macOS Keychain
apimount --auth-bearer 'keychain:apimount/github' ...
```

---

## Reliability

### Retry

Failed requests to idempotent endpoints (GET, HEAD, PUT, DELETE, OPTIONS) are retried automatically with exponential backoff and full jitter. Retryable status codes: 429, 502, 503, 504.

```bash
apimount --max-retries 5 --spec ./api.yaml get /items
```

### Rate limiting

A per-host token bucket prevents overwhelming upstream APIs. Defaults: 10 req/s, burst 20. The limiter honours `Retry-After` and `X-RateLimit-*` response headers to dynamically adjust pacing.

```bash
apimount --rate-limit 5 --rate-burst 10 --spec ./api.yaml get /items
```

### Pagination

Paginated GET responses are automatically fetched and merged into a single JSON array. Four strategies are auto-detected:

| Strategy | Detection |
|---|---|
| **Link** | `Link: <url>; rel="next"` header |
| **Cursor** | Response field named `cursor` / `next_cursor` / `after` / `continuation_token` |
| **Offset/Limit** | Query params `offset` + `limit` |
| **Page/Size** | Query params `page` + `per_page` / `size` |

```bash
apimount --max-pages 50 --spec ./api.yaml get /items
```

An `X-Apimount-Pages` header is added to the merged response indicating how many pages were fetched.

### Request validation

Validate request bodies against the operation's JSON Schema before sending:

```bash
apimount --validate --spec ./petstore.yaml post /pet --body '{"name":"Rex"}'
```

Validation errors report the exact path and reason for each violation, so you catch mistakes before they hit the server.

---

## Flags (global)

```
--config string         config file (default: ~/.apimount.yaml)
--spec string           path or URL to OpenAPI spec (required)
--base-url string       override base URL from spec
--profile string        use a named profile from config file
--timeout duration      HTTP request timeout (default 30s)
--auth-bearer string    Bearer token
--auth-basic string     Basic auth user:password
--auth-apikey string    API key value
--auth-apikey-param     API key header/param name
--max-retries int       max retry attempts for idempotent requests (default 3)
--rate-limit float      per-host requests per second, 0 = unlimited (default 10)
--rate-burst int        per-host burst size (default 20)
--max-pages int         max pages for paginated responses (default 100)
--validate              validate request body against schema before sending
--verbose               debug logging
```

Per-command flags (body, query, header) are listed on each subcommand via `--help`.

---

## Development

```bash
make build         # bin/apimount
make test          # race detector
make lint          # golangci-lint v2 + depguard layering rules
make vulncheck     # govulncheck
```

See [apimount_spec.md](apimount_spec.md) for the full engineering spec, architecture, and phased implementation order.
