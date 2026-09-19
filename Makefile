.DEFAULT_GOAL := help

APP        := himorime
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X github.com/nao1215/himorime/internal/buildinfo.Version=$(VERSION)
PKGS       := ./...
GOLANGCI   := v2.13.2
ATAGO      := v0.22.0
GORELEASER := v2.16.0
ACTIONLINT := v1.7.12
FUZZTIME   ?= 10s

.PHONY: build
build: ## Build the himorime binary into ./himorime
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(APP) .

.PHONY: install
install: ## Install himorime into $(go env GOPATH)/bin
	CGO_ENABLED=0 go install -trimpath -ldflags '$(LDFLAGS)' .

.PHONY: clean
clean: ## Remove build, test and site artifacts
	rm -rf $(APP) $(APP).exe dist completions cover.out cover.html website/public website/resources

.PHONY: fmt
fmt: ## Format Go code
	gofmt -s -w .

.PHONY: vet
vet: ## Run go vet
	go vet $(PKGS)

.PHONY: lint
lint: ## Run golangci-lint for every operating system himorime builds for
	@for os in linux darwin windows freebsd openbsd netbsd; do \
		echo "== GOOS=$$os"; GOOS=$$os golangci-lint run $(PKGS) || exit 1; \
	done

.PHONY: test
test: ## Run unit tests with coverage (cover.out)
	go test -cover -coverprofile=cover.out $(PKGS)

.PHONY: test-race
test-race: ## Run unit tests with the race detector
	go test -race -timeout 15m $(PKGS)

.PHONY: coverage
coverage: test ## Write cover.html and print the total coverage
	go tool cover -html=cover.out -o cover.html
	go tool cover -func=cover.out | tail -1

.PHONY: e2e
e2e: ## Build himorime and run the atago end-to-end suite (needs atago on PATH)
	go run ./test/e2e/run

.PHONY: fuzz
fuzz: ## Run every fuzz target for FUZZTIME (default 10s)
	go test -run '^$$' -fuzz '^FuzzLoad$$' -fuzztime $(FUZZTIME) ./internal/config
	go test -run '^$$' -fuzz '^FuzzParseDuration$$' -fuzztime $(FUZZTIME) ./internal/config
	go test -run '^$$' -fuzz '^FuzzParseThreshold$$' -fuzztime $(FUZZTIME) ./internal/metric
	go test -run '^$$' -fuzz '^FuzzExpand$$' -fuzztime $(FUZZTIME) ./internal/config
	go test -run '^$$' -fuzz '^FuzzCompare$$' -fuzztime $(FUZZTIME) ./internal/stats
	go test -run '^$$' -fuzz '^FuzzReportFormats$$' -fuzztime $(FUZZTIME) ./internal/report
	go test -run '^$$' -fuzz '^FuzzEscapeMarkdown$$' -fuzztime $(FUZZTIME) ./internal/report
	go test -run '^$$' -fuzz '^FuzzResolveBase$$' -fuzztime $(FUZZTIME) ./internal/ghactions

.PHONY: calibration
calibration: ## Print the regression classifier's verdict rates on synthetic data (1,000 trials per scenario)
	HIMORIME_CALIBRATION=full go test -run '^TestCalibration$$' -count=1 -v ./internal/stats

.PHONY: docs
docs: ## Regenerate the generated sections of README.md and website/content
	UPDATE_DOCS=1 go test ./internal/docgen -run '^TestGeneratedDocsInSync$$' -count=1

.PHONY: website
website: ## Build the documentation site into website/public (needs hugo)
	cd website && hugo --gc --minify --cleanDestinationDir

.PHONY: website-serve
website-serve: ## Serve the documentation site locally
	cd website && hugo server

.PHONY: vuln
vuln: ## Run govulncheck
	govulncheck $(PKGS)

.PHONY: actionlint
actionlint: ## Lint the GitHub Actions workflows and example workflows
	actionlint .github/workflows/*.yml examples/github-actions/benchmark.yml examples/github-actions/comment.yml

.PHONY: release-check
release-check: ## Validate .goreleaser.yml
	goreleaser check

.PHONY: release-smoke
release-smoke: ## Build a snapshot release locally and smoke-test the artifacts
	goreleaser release --snapshot --clean --skip=publish,sign
	go run ./scripts/smoke dist

.PHONY: bench
bench: ## Run the dogfood suite that measures himorime itself
	go run . run bench

.PHONY: bench-docs
bench-docs: ## Regenerate the overhead table in website/content/metrics.md
	go run -ldflags '$(LDFLAGS)' . run --quiet --filter '^collector overhead$$' --format markdown --output website/content/metrics.md --section overhead bench

.PHONY: check
check: fmt vet lint test test-race e2e ## Run what CI runs, except the release and site jobs

.PHONY: tools
tools: ## Install the developer tools used by this repository
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI)
	go install github.com/nao1215/atago@$(ATAGO)
	go install github.com/goreleaser/goreleaser/v2@$(GORELEASER)
	go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT)
	go install golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
