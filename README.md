# apimount

**A universal OpenAPI adapter.** Point it at any OpenAPI 3.0 / 3.1 spec and call every operation through whichever surface you need — CLI, MCP server, WebDAV, or NFS. All surfaces share one execution core that handles auth, retries, rate-limits, pagination, schema validation, and audit.

```bash
# CLI — zero setup, works everywhere
apimount --spec ./petstore.yaml --base-url $URL get /pet/42
apimount --spec ./petstore.yaml --base-url $URL post /pet --body '{"name":"Rex","photoUrls":[]}'
```

> **Status.** Phase 1 (core refactor) and Phase 2 (CLI-first UX) are shipped. Phase 3 (enterprise auth) is in progress. The MCP, WebDAV, and NFS frontends are on the roadmap — see [apimount_spec.md §14](apimount_spec.md#L734-L768) for the implementation order.

---

## Why apimount

Every API consumer re-writes the same plumbing: auth, retries, pagination, rate-limit backoff, request validation, audit logs. apimount turns a spec into a runnable surface so you get all of it for free, through the interface that fits your workflow:

| Surface | Use case | Status |
|---|---|---|
| **CLI** | Scripting, CI, humans in a terminal — zero setup, no mount | ✅ shipped |
| **MCP server** | Expose an API as first-class tools to Claude Code / Desktop / Cursor / any MCP client | 🗺️ planned (Phase 5) |
| **WebDAV server** | Browse an API from Finder / Explorer / any HTTP GUI, no kernel driver | 🗺️ planned (Phase 5) |
| **NFS server** | Cross-platform mount (Linux / macOS / Windows) with no kernel driver, runs in containers | 🗺️ planned (Phase 5) |
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
