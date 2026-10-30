BINARY  := bsk
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint e2e

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/bsk

test:
	go test ./...

lint:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed on:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go vet -tags e2e ./test/e2e/...

# End-to-end tests against the disposable local BookStack of test/e2e/
# (Docker); never run by "make test" or against a real instance.
e2e:
	test/e2e/up.sh
	set -a; . test/e2e/.env.local; set +a; \
	BSK_E2E=1 go test -tags e2e -count=1 ./test/e2e/...
