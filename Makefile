.PHONY: build build-linux test test-verbose vet lint vulncheck clean install

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
