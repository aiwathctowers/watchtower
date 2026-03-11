BINARY_NAME := watchtower
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
OAUTH_ID    ?= $(WATCHTOWER_OAUTH_CLIENT_ID)
OAUTH_SECRET?= $(WATCHTOWER_OAUTH_CLIENT_SECRET)
LDFLAGS     := -ldflags "-X watchtower/cmd.Version=$(VERSION) -X watchtower/cmd.Commit=$(COMMIT) -X watchtower/cmd.BuildDate=$(BUILD_DATE) -X watchtower/internal/auth.DefaultClientID=$(OAUTH_ID) -X watchtower/internal/auth.DefaultClientSecret=$(OAUTH_SECRET)"

.PHONY: build test lint install clean app test-swift

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) .

app:
	./scripts/build-app.sh $(VERSION)

test:
	go test ./... -v

test-swift:
	cd WatchtowerDesktop && swift test

lint:
	go vet ./...

install:
	go install $(LDFLAGS) .

clean:
	rm -f $(BINARY_NAME)
	rm -rf build/
