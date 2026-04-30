-include .env
export WATCHTOWER_OAUTH_CLIENT_ID WATCHTOWER_OAUTH_CLIENT_SECRET WATCHTOWER_GOOGLE_CLIENT_ID WATCHTOWER_GOOGLE_CLIENT_SECRET WATCHTOWER_JIRA_CLIENT_ID WATCHTOWER_JIRA_CLIENT_SECRET

BINARY_NAME := watchtower
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
OAUTH_ID    ?= $(WATCHTOWER_OAUTH_CLIENT_ID)
OAUTH_SECRET?= $(WATCHTOWER_OAUTH_CLIENT_SECRET)
GOOGLE_ID   ?= $(WATCHTOWER_GOOGLE_CLIENT_ID)
GOOGLE_SECRET?= $(WATCHTOWER_GOOGLE_CLIENT_SECRET)
JIRA_ID     ?= $(WATCHTOWER_JIRA_CLIENT_ID)
JIRA_SECRET ?= $(WATCHTOWER_JIRA_CLIENT_SECRET)
LDFLAGS     := -ldflags "-X watchtower/cmd.Version=$(VERSION) -X watchtower/cmd.Commit=$(COMMIT) -X watchtower/cmd.BuildDate=$(BUILD_DATE) -X watchtower/internal/auth.DefaultClientID=$(OAUTH_ID) -X watchtower/internal/auth.DefaultClientSecret=$(OAUTH_SECRET) -X watchtower/internal/calendar.DefaultGoogleClientID=$(GOOGLE_ID) -X watchtower/internal/calendar.DefaultGoogleClientSecret=$(GOOGLE_SECRET) -X watchtower/internal/jira.DefaultJiraClientID=$(JIRA_ID) -X watchtower/internal/jira.DefaultJiraClientSecret=$(JIRA_SECRET)"

.PHONY: build test lint lint-swift lint-all install clean app app-dev dmg test-swift sentrux-check

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) .

app dmg:
	./scripts/build-app.sh $(VERSION)

app-dev:
	./scripts/build-app.sh --dev $(VERSION)

test:
	go test ./... -v

test-swift:
	cd WatchtowerDesktop && swift test

lint:
	golangci-lint run ./...

lint-swift:
	cd WatchtowerDesktop && swiftlint lint --strict

lint-all: lint lint-swift

install:
	go install $(LDFLAGS) .

clean:
	rm -f $(BINARY_NAME)
	rm -rf build/

# Architectural rules check via sentrux. Not wired into `test` — it currently
# reports CC/length debt (see .sentrux/rules.toml). Run manually or in PR review.
SENTRUX ?= $(shell command -v sentrux 2>/dev/null || echo /opt/homebrew/bin/sentrux)
sentrux-check:
	$(SENTRUX) check .
