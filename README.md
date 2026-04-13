# apimount

Mount any OpenAPI 3.0/3.1 spec as a FUSE filesystem. Interact with APIs using standard Unix tools — `ls`, `cat`, `echo` — no HTTP client code required.

```bash
apimount --spec ./github-api.yaml --base-url https://api.github.com \
         --mount /mnt/github --auth-bearer $TOKEN

ls /mnt/github/repos/
cat /mnt/github/repos/myorg/myrepo/.data
echo '{"title":"bug","body":"details"}' > /mnt/github/issues/.post
cat /mnt/github/issues/.response
```

---

## Installation

### Prerequisites

**macOS** — install [macFUSE](https://osxfuse.github.io/) (free, open source):
```bash
brew install --cask macfuse
# Then approve the kernel extension in System Settings → Privacy & Security
```

**Linux** — install FUSE3:
```bash
sudo apt install fuse3          # Debian/Ubuntu
sudo dnf install fuse3          # Fedora
```

### Build from source

```bash
git clone https://github.com/apimount/apimount
cd apimount
make build          # → bin/apimount
make install        # installs to $GOPATH/bin
```

---

## Usage

### Mount

```bash
apimount \
  --spec ./petstore.yaml \
  --base-url https://petstore3.swagger.io/api/v3 \
  --mount /tmp/petstore
```

### Unmount

```bash
# macOS
umount /tmp/petstore

# Linux
fusermount -u /tmp/petstore
```

### Dry run (print filesystem tree without mounting)

```bash
apimount --spec ./petstore.yaml --dry-run
apimount tree --spec ./petstore.yaml --group-by path
```

### Validate a spec

```bash
apimount validate --spec https://petstore3.swagger.io/api/v3/openapi.json
```

---

## Filesystem layout

Every API path becomes a directory. Every HTTP method on that path becomes a dotfile:

```
/mnt/petstore/
├── pet/
│   ├── .data          ← cat: GET /pet
│   ├── .post          ← echo '{"name":"Rex",...}' > .post  (POST /pet)
│   ├── .schema        ← cat: JSON schema of POST request body
│   ├── .response      ← cat: last response from any operation
│   ├── .help          ← cat: human-readable description
│   ├── findByStatus/
│   │   ├── .data      ← cat: GET /pet/findByStatus
│   │   └── .query     ← echo "status=available" > .query  (GET with params)
│   └── {petId}/       ← path parameter template
│       ├── .data      ← cat /mnt/petstore/pet/1/.data  → GET /pet/1
│       ├── .put       ← write body → PUT /pet/{petId}
│       └── .delete    ← echo x > .delete → DELETE /pet/{petId}
```

### File roles

| File | Read | Write |
|---|---|---|
| `.data` | Execute GET, return body | — |
| `.post` | Return last POST response | Execute POST with body |
| `.put` | Return last PUT response | Execute PUT with body |
| `.patch` | Return last PATCH response | Execute PATCH with body |
| `.delete` | Return last DELETE response | Execute DELETE |
| `.query` | Execute GET with stored params | Store query params (`key=val&k2=v2`) |
| `.schema` | JSON schema of request body | — |
| `.response` | Last raw response (any operation) | — |
| `.help` | Human-readable description | — |

### Path parameters

Navigate into any name under a `{param}` directory — apimount dynamically binds the value:

```bash
# Resolves to GET /pet/42
cat /mnt/petstore/pet/42/.data

# Resolves to DELETE /pet/42
echo x > /mnt/petstore/pet/42/.delete

# Nested params work too: GET /store/order/5
cat /mnt/petstore/store/order/5/.data
```

---

## Auth

```bash
# Bearer token
apimount --spec ... --auth-bearer ghp_xxxx

# Basic auth
apimount --spec ... --auth-basic user:password

# API key (header)
apimount --spec ... --auth-apikey mykey
# Override the header name if not in spec:
apimount --spec ... --auth-apikey mykey --auth-apikey-param X-Custom-Key
```

---

## Flags

```
--spec string           Path or URL to OpenAPI spec (required)
--mount string          Mount point directory
--base-url string       Override base URL from spec
--auth-bearer string    Bearer token
--auth-basic string     Basic auth user:password
--auth-apikey string    API key value
--auth-apikey-param     API key header/param name
--timeout duration      HTTP timeout (default 30s)
--cache-ttl duration    GET cache TTL, 0=off (default 30s)
--cache-max-mb int      Max cache size MB (default 50)
--group-by string       Tree grouping: tags|path|flat (default tags)
--pretty                Pretty-print JSON (default true)
--read-only             Disallow all write operations
--allow-other           FUSE allow_other (needs /etc/fuse.conf on Linux)
--dry-run               Print tree without mounting
--verbose               Debug logging
--profile string        Use a named profile from ~/.apimount.yaml
```

---

## Config file

Save profiles to `~/.apimount.yaml`:

```yaml
profiles:
  petstore:
    spec: https://petstore3.swagger.io/api/v3/openapi.json
    base-url: https://petstore3.swagger.io/api/v3
    cache-ttl: 30s
    group-by: path

  github:
    spec: https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.yaml
    base-url: https://api.github.com
    auth-bearer: ghp_xxxxxxxxxxxx
    cache-ttl: 60s
```

Use with:
```bash
apimount --profile petstore --mount /tmp/petstore
```

---

## Known limitations

1. **macOS requires macFUSE** — install from [osxfuse.github.io](https://osxfuse.github.io/) and approve the kext in System Settings.
2. **OpenAPI 3.x only** — Swagger 2.0 is rejected with a clear error.
3. **Pagination** — only page 1 is returned via `.data`; use `.query` to set `page=N`.
4. **OAuth2** — obtain the token manually and pass via `--auth-bearer`.
5. **Binary responses** — use `cp` instead of `cat` to save binary files.
6. **Multipart form data** — not supported for writes.
7. **Concurrent writes** — writing to the same file from multiple processes simultaneously has undefined behaviour.

---

## Development

```bash
make test          # run tests with race detector
make build         # build binary → bin/apimount
make demo          # mount petstore locally (requires FUSE)
make lint          # golangci-lint
```
