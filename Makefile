MODULE  := github.com/aphexddb/omarket
VERSION := $(shell cat VERSION)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%d)
BIN     := omarket$(shell go env GOEXE)

TAG            := v$(VERSION)
RELEASE_BRANCH := master

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: build test examples release-check release

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/omarket

test:
	go test ./...

examples:
	$(MAKE) -C examples demo

# Refuse to tag anything a release cannot be cut from. Each gate is a way this
# has gone wrong before: a dirty tree tags code that was never committed, a
# branch out of sync with origin tags a commit CI will not build, and a tag
# already on origin cannot be moved once a release was published from it.
# Existence is checked against origin rather than local refs, because a stale
# local tag is exactly the case a local check would miss.
release-check:
	@echo "$(VERSION)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' \
		|| { echo "VERSION '$(VERSION)' is not a semver version"; exit 1; }
	@git diff --quiet && git diff --cached --quiet \
		|| { echo "working tree is dirty - commit or stash first"; exit 1; }
	@test "$$(git rev-parse --abbrev-ref HEAD)" = "$(RELEASE_BRANCH)" \
		|| { echo "on $$(git rev-parse --abbrev-ref HEAD), not $(RELEASE_BRANCH)"; exit 1; }
	@git fetch --quiet origin $(RELEASE_BRANCH)
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse FETCH_HEAD)" \
		|| { echo "$(RELEASE_BRANCH) is out of sync with origin - pull or push first"; exit 1; }
	@! git rev-parse -q --verify refs/tags/$(TAG) >/dev/null \
		|| { echo "$(TAG) already exists locally"; exit 1; }
	@test -z "$$(git ls-remote --tags origin 'refs/tags/$(TAG)')" \
		|| { echo "$(TAG) already exists on origin"; exit 1; }
	@echo "$(TAG) is ready to cut from $$(git rev-parse --short HEAD)"

# Tag and push. .github/workflows/release.yml takes it from there: it re-checks
# that VERSION matches the tag, then runs GoReleaser. The tag is annotated so it
# is a real tag object with an author and date; SIGN=1 signs it instead, and
# YES=1 skips the prompt for non-interactive use.
release: release-check test
	@if [ -z "$(YES)" ]; then \
		printf 'tag and push %s? [y/N] ' '$(TAG)'; \
		read reply; \
		[ "$$reply" = "y" ] || { echo "aborted"; exit 1; }; \
	fi
	git tag $(if $(SIGN),-s,-a) $(TAG) -m "$(TAG)"
	git push origin $(TAG)
	@echo "pushed $(TAG) - follow it with: gh run watch --workflow=release.yml"
