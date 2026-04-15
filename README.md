# apimount

**A universal OpenAPI adapter.** Point it at any OpenAPI 3.0 / 3.1 spec and call every operation through whichever surface you need — CLI, MCP server, WebDAV, NFS, or (optionally) FUSE. All surfaces share one execution core that handles auth, retries, rate-limits, pagination, schema validation, and audit.

```bash
# CLI — zero setup, works everywhere
apimount --spec ./petstore.yaml --base-url $URL get /pet/42
apimount --spec ./petstore.yaml --base-url $URL post /pet --body '{"name":"Rex","photoUrls":[]}'
```

> **Status.** Phase 1 (core refactor) and Phase 2 (CLI-first UX) are shipped. The MCP, WebDAV, NFS, and FUSE frontends listed below are on the roadmap — see [apimount_spec.md §14](apimount_spec.md#L734-L768) for the implementation order. The v1 FUSE code path still runs via `apimount serve fuse` for users who already depend on it.

---

## Why apimount

Every API consumer re-writes the same plumbing: auth, retries, pagination, rate-limit backoff, request validation, audit logs. apimount turns a spec into a runnable surface so you get all of it for free, through the interface that fits your workflow:

| Surface | Use case | Status |
|---|---|---|
| **CLI** | Scripting, CI, humans in a terminal — zero setup, no mount | ✅ shipped |
| **MCP server** | Expose an API as first-class tools to Claude Code / Desktop / Cursor / any MCP client | 🗺️ planned (Phase 5) |
| **WebDAV server** | Browse an API from Finder / Explorer / any HTTP GUI, no kernel driver | 🗺️ planned (Phase 5) |
| **NFS server** | Cross-platform mount (Linux / macOS / Windows) with no macFUSE, runs in containers | 🗺️ planned (Phase 5) |
| **FUSE** | Lowest-latency local mount for advanced users who already have macFUSE / libfuse3 | ⚠️ v1 code, carried forward |
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

### FUSE prerequisites (only if you use `apimount serve fuse`)

The CLI works on every OS with no extra install. FUSE is the only surface that needs a kernel driver, and it's optional.

**macOS** — [macFUSE](https://osxfuse.github.io/):
```bash
brew install --cask macfuse
# Then approve the kernel extension in System Settings → Privacy & Security
```

**Linux** — FUSE3:
```bash
sudo apt install fuse3          # Debian/Ubuntu
sudo dnf install fuse3          # Fedora
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

Reports OS / arch, Go runtime, spec reachability, and config file presence. (Also detects FUSE userspace if installed — informational only; the CLI does not need it.)

---

## Auth

```bash
apimount --auth-bearer ghp_xxxx ...
apimount --auth-basic user:password ...
apimount --auth-apikey mykey --auth-apikey-param X-Custom-Key ...
```

Enterprise auth (OAuth2 flows, SigV4, mTLS, Vault / Keychain / 1Password secret providers) is scoped for Phase 3 — see [apimount_spec.md §14](apimount_spec.md#L750-L755).

---

## FUSE (optional)

Shipped for users who already depended on v1's FUSE mount. Everything the CLI does, FUSE also does — through the filesystem:

```bash
apimount serve fuse \
  --spec ./petstore.yaml \
  --base-url https://petstore3.swagger.io/api/v3 \
  --mount /tmp/petstore

cat /tmp/petstore/pet/42/.data
echo '{"name":"Rex","photoUrls":[]}' > /tmp/petstore/pet/.post
```

v1 users can keep calling `apimount --spec S --mount M` verbatim; it prints a deprecation notice and dispatches to `serve fuse`. See [apimount_spec.md §16](apimount_spec.md#L807-L814).

### Unmount

```bash
umount /tmp/petstore             # macOS
fusermount -u /tmp/petstore      # Linux
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

Per-command flags (body, query, header, mount, cache, grouping) are listed on each subcommand via `--help`.

---

## Development

```bash
make build         # bin/apimount
make test          # race detector
make lint          # golangci-lint v2 + depguard layering rules
make vulncheck     # govulncheck
```

See [apimount_spec.md](apimount_spec.md) for the full engineering spec, architecture, and phased implementation order.
