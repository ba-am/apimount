.PHONY: build build-linux test test-verbose vet lint vulncheck clean install demo unmount

build:
	go build -o bin/apimount ./cmd/apimount

# Static Linux binary (for CI or cross-compile from macOS)
build-linux:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o bin/apimount-linux ./cmd/apimount

test:
	go test ./... -race -count=1

test-verbose:
	go test ./... -v -race

vet:
	go vet ./...

lint:
	golangci-lint run ./...

vulncheck:
	govulncheck ./...

clean:
	rm -rf bin/

install:
	go install ./cmd/apimount

# Mount petstore locally for manual testing (requires macFUSE or libfuse3)
demo:
	mkdir -p mnt
	./bin/apimount \
		--spec testdata/petstore.yaml \
		--base-url https://petstore3.swagger.io/api/v3 \
		--mount mnt \
		--verbose

# Unmount (works on macOS and Linux)
unmount:
	fusermount -u mnt 2>/dev/null || umount mnt 2>/dev/null || diskutil unmount mnt 2>/dev/null || true
	rmdir mnt 2>/dev/null || true
