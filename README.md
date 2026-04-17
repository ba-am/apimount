# apimount

**Turn any OpenAPI spec into a working API client — instantly.**

No SDK generation. No boilerplate. No per-API setup. Point apimount at an OpenAPI 3.0/3.1 spec and interact with every endpoint through the CLI, as MCP tools for AI agents, or as a mounted filesystem.

```bash
apimount --spec ./petstore.yaml --base-url https://petstore3.swagger.io/api/v3 get /pet/42
```

---

## The problem

Every time you integrate with a new API, you re-build the same plumbing: authentication, retries on transient failures, pagination across hundreds of pages, rate-limit backoff, request validation. You write it once for curl scripts, again for your Python service, again for your CI pipeline.

API code generators (openapi-generator, oapi-codegen) solve part of this — but they produce static code you have to regenerate on every spec change, and they don't handle runtime concerns like retries or pagination. Tools like HTTPie and curl are great for one-off requests, but they don't understand your spec — you're on your own for auth flows, parameter validation, and multi-page responses.

**apimount takes a different approach.** It reads the spec at runtime and gives you a fully-featured API client with auth, retries, rate-limiting, pagination, and validation — all built in, zero code to write.

---

## What you can do with it

### Call any API from the terminal

```bash
# Fetch a resource
apimount --spec ./github.yaml --base-url https://api.github.com \
  --auth-bearer ghp_xxxx get /repos/torvalds/linux

# Create a resource
apimount --spec ./petstore.yaml --base-url $URL \
  post /pet --body '{"name":"Rex","photoUrls":[]}'

# Delete
apimount --spec ./petstore.yaml --base-url $URL delete /pet/42
```

Works with any API that has an OpenAPI spec. That includes GitHub, Stripe, Twilio, Slack, AWS API Gateway, and thousands of others — most publish their specs publicly.

### Give Claude and other AI agents access to any API

apimount can serve every API operation as an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) tool. This means Claude Code, Claude Desktop, Cursor, or any MCP-compatible agent can call API endpoints directly — with proper auth, retries, and validation handled automatically.

```bash
# Add as an MCP server to Claude Code
claude mcp add apimount -- apimount serve mcp --spec ./github.yaml --base-url https://api.github.com --auth-bearer ghp_xxxx
```

Claude immediately sees every operation in the spec as a callable tool. No wrapper code, no custom server — just point at a spec.

### Browse an API as a filesystem

Mount any API as a **WebDAV** or **NFS** share and explore it from Finder, Explorer, or any file manager:

```bash
# WebDAV — connect from Finder ("Connect to Server") or Explorer ("Map Network Drive")
apimount serve webdav --spec ./petstore.yaml --base-url $URL --addr :8080

# NFS — mount from any OS, no kernel driver
apimount serve nfs --spec ./petstore.yaml --base-url $URL --addr :2049
sudo mount -t nfs -o vers=3,nolock,tcp,port=2049 127.0.0.1:/ /mnt/api
```

Every endpoint becomes a file. `cat /mnt/api/pets/.data` executes a GET. Schema files show you what the API expects. It's a different way to explore and understand an API.

---

## Quick start

### 1. Install

```bash
git clone https://github.com/ba-am/apimount
cd apimount
make build    # produces bin/apimount
```

Or install directly:

```bash
make install  # installs to $GOPATH/bin
```

Requires Go 1.25+.

### 2. Try it with the Petstore demo API

The [Swagger Petstore](https://petstore3.swagger.io/) is a free demo API — perfect for testing:

```bash
export SPEC=https://petstore3.swagger.io/api/v3/openapi.json
export URL=https://petstore3.swagger.io/api/v3

# List available operations
apimount tree --spec $SPEC

# Get a pet
apimount --spec $SPEC --base-url $URL get /pet/1

# Add a pet
apimount --spec $SPEC --base-url $URL post /pet \
  --body '{"id":0,"name":"Buddy","photoUrls":["https://example.com/buddy.jpg"],"status":"available"}'

# Validate a request body before sending
apimount --spec $SPEC --base-url $URL --validate post /pet \
  --body '{"invalid":"missing required fields"}'

# Explore the spec structure
apimount validate --spec $SPEC
```

### 3. Use with a real API (GitHub example)

```bash
# Create a profile so you don't repeat flags
cat >> ~/.apimount.yaml << 'EOF'
profiles:
  github:
    spec: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.yaml
    base-url: https://api.github.com
    auth-bearer: ghp_your_token_here
EOF

# Now use the profile
apimount --profile github get /repos/torvalds/linux
apimount --profile github get /repos/torvalds/linux/issues --query state=open
```

---

## How it works

apimount has one execution core shared by every interface:

```
                ┌─────────────────────────────────────────────┐
  CLI ──────────┤                                             │
  MCP server ───┤   Validate → Paginate → Retry → RateLimit  │──→ Upstream API
  WebDAV ───────┤         (middleware pipeline)               │
  NFS ──────────┤                                             │
                └─────────────────────────────────────────────┘
```

Every request — no matter which interface you use — goes through the same middleware pipeline. Auth is injected, retries happen on 429/502/503/504, rate limits are respected, paginated responses are merged, and request bodies are validated against the schema. You get production-grade API behaviour from a single binary.

---

## Authentication

apimount supports the auth methods you'll encounter in real APIs:

```bash
# Bearer token (GitHub, Stripe, most SaaS APIs)
apimount --auth-bearer $TOKEN ...

# Basic auth
apimount --auth-basic user:password ...

# API key in a custom header
apimount --auth-apikey $KEY --auth-apikey-param X-Api-Key ...

# OAuth2 client credentials (machine-to-machine)
apimount --auth-oauth2-client-id $ID --auth-oauth2-client-secret 'env:SECRET' \
  --auth-oauth2-token-url https://issuer.example.com/oauth/token ...

# OAuth2 device-code flow (interactive browser login)
apimount auth login --auth-oauth2-client-id $ID \
  --auth-oauth2-token-url $TOKEN_URL --auth-oauth2-device-url $DEVICE_URL \
  --profile myapi

# mTLS (internal services, zero-trust networks)
apimount --auth-mtls-cert ./client.crt --auth-mtls-key ./client.key ...

# AWS SigV4 (API Gateway, S3, any AWS service)
apimount --auth-sigv4-access-key 'env:AWS_ACCESS_KEY_ID' \
  --auth-sigv4-secret-key 'env:AWS_SECRET_ACCESS_KEY' --auth-sigv4-region us-east-1 ...
```

### Keeping secrets safe

Credentials never need to be passed as plain text. Use references instead:

| Prefix | Source |
|---|---|
| `env:VAR_NAME` | Environment variable |
| `file:/path/to/secret` | File contents (must be chmod 0600) |
| `op:op://vault/item/field` | 1Password CLI |
| `keychain:service/account` | macOS Keychain |

```bash
# Token from 1Password — never touches shell history
apimount --auth-bearer 'op:op://Private/github-token/credential' ...

# Token from environment variable
apimount --auth-bearer 'env:GITHUB_TOKEN' ...
```

---

## Built-in reliability

These features work automatically — no configuration required:

### Retry with backoff

Failed requests to safe endpoints (GET, HEAD, PUT, DELETE) are retried automatically with exponential backoff and jitter. Retries happen on 429, 502, 503, and 504 status codes.

```bash
apimount --max-retries 5 ...    # default: 3
```

### Rate limiting

A per-host token bucket prevents you from overwhelming upstream APIs. It reads `Retry-After` and `X-RateLimit-*` headers from responses and adjusts automatically.

```bash
apimount --rate-limit 5 --rate-burst 10 ...    # default: 10 req/s, burst 20
```

### Auto-pagination

Paginated responses are automatically fetched and merged into a single JSON array. apimount detects the pagination strategy from the response:

- **Link header** — follows `rel="next"` links (GitHub, many REST APIs)
- **Cursor** — detects `cursor`, `next_cursor`, `after`, `continuation_token` fields
- **Offset/Limit** — walks `offset` + `limit` query params
- **Page/Size** — walks `page` + `per_page` query params

```bash
apimount --max-pages 50 ...    # default: 100
```

### Request validation

Catch mistakes before they hit the server:

```bash
apimount --validate post /pet --body '{"name":"Rex"}'
# Error: /photoUrls: required field missing
```

---

## Profiles

Save connection details for APIs you use often:

```yaml
# ~/.apimount.yaml
profiles:
  github:
    spec: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.yaml
    base-url: https://api.github.com
    auth-bearer: env:GITHUB_TOKEN
  stripe:
    spec: ./stripe-openapi.yaml
    base-url: https://api.stripe.com
    auth-bearer: env:STRIPE_SECRET_KEY
  internal:
    spec: ./our-api.yaml
    base-url: https://api.internal.company.com
    auth-mtls-cert: ./client.crt
    auth-mtls-key: ./client.key
```

```bash
apimount --profile github get /user
apimount --profile stripe get /v1/customers
apimount profile list            # show all profiles
apimount profile show github     # show details (auth values redacted)
```

---

## All flags

```
Connection:
  --spec string           Path or URL to OpenAPI spec (required)
  --base-url string       Override base URL from the spec
  --profile string        Use a named profile from ~/.apimount.yaml
  --config string         Config file path (default: ~/.apimount.yaml)
  --timeout duration      HTTP request timeout (default: 30s)

Authentication:
  --auth-bearer string    Bearer token
  --auth-basic string     Basic auth (user:password)
  --auth-apikey string    API key value
  --auth-apikey-param     API key header name
  --auth-oauth2-client-id, --auth-oauth2-client-secret, --auth-oauth2-token-url, --auth-oauth2-scopes
  --auth-mtls-cert, --auth-mtls-key, --auth-mtls-ca
  --auth-sigv4-access-key, --auth-sigv4-secret-key, --auth-sigv4-region, --auth-sigv4-service

Reliability:
  --max-retries int       Max retry attempts (default: 3)
  --rate-limit float      Requests per second per host (default: 10)
  --rate-burst int        Burst size (default: 20)
  --max-pages int         Max pages for pagination (default: 100)
  --validate              Validate request body before sending

Output:
  --verbose               Debug logging
```

Per-command flags (`--body`, `--query`, `--header`, `--body-file`) are listed via `apimount <command> --help`.

---

## When is apimount useful?

**Scripting and CI/CD** — Call any API from shell scripts without writing a custom client. Retries and auth are built in.

**API exploration** — Point at a spec and start calling endpoints. `apimount tree` shows you what's available; `--validate` catches mistakes before they hit the server.

**AI agent integration** — Give Claude or any MCP-compatible agent access to hundreds of API operations instantly. No wrapper code needed.

**Internal tools** — Mount internal APIs as filesystems for non-technical users, or use profiles to standardize how your team talks to shared services.

**Testing and debugging** — Quickly hit endpoints during development without setting up Postman collections or writing throwaway scripts.

---

## License

MIT
