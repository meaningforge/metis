.PHONY: check release-check golden-path docs-check ci-contract-check test test-unit test-samples test-conformance test-e2e test-duckdb-backend test-engine-clickhouse test-engine-clickhouse-container test-engine-doris test-engine-doris-container semantic-correctness-coverage semantic-target-evidence okf-readiness-conformance fmt fmt-check vet validate ossie-sync smoke docker-build metis-build metis-smoke s2sbench-build s2sbench-boundary-check ossie-conformance clickhouse-conformance reference-conformance

# Standard developer/CI correctness gate. Keep the release-facing quickstart
# executable here so README/CLI/path drift is caught on ordinary pull requests.
check: docs-check licenses-check ci-contract-check vet test golden-path

# Release gate: everything required before producing distributable artifacts.
# Real-engine execution remains a CI job because it requires external services.
release-check: fmt-check check test-e2e
	@tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
	go build -trimpath \
		-ldflags "-X github.com/meaningforge/metis/version.Version=v0.1.0-test -X github.com/meaningforge/metis/version.Commit=release-check -X github.com/meaningforge/metis/version.Date=1970-01-01T00:00:00Z" \
		-o "$$tmp/metis" ./cmd/metis; \
	"$$tmp/metis" version | grep -q '"version":"v0.1.0-test"'; \
	"$$tmp/metis" version | grep -q '"commit":"release-check"'; \
	"$$tmp/metis" version | grep -q '"date":"1970-01-01T00:00:00Z"'

golden-path:
	bash tools/verify-golden-path.sh

docs-check:
	bash tools/check-docs.sh

ci-contract-check:
	python3 tools/test_release_checks.py
	bash tools/ci/real-engine-policy.sh
	bash tools/ci/real-engine-registration.sh
	bash tools/ci/golden-path-contract_test.sh
	bash tools/ci/metric-evaluation-authority.sh
	bash tools/ci/semantic-node-vocabulary.sh
	bash tools/ci/backend-extension-boundaries.sh
	$(MAKE) s2sbench-boundary-check

# Fast in-process Go tests. Conformance and real-engine packages have dedicated
# targets below so each correctness layer runs exactly once.
test-unit:
	go test $$(go list ./... | grep -v '/tests/conformance/' | grep -v '/tests/engine/')

# Repository samples are executable contracts, not documentation snippets.
test-samples:
	bash tests/samples/run.sh

# Shared semantic/query and target-dialect compiler correctness. Architecture
# and support decisions are executable contracts under tests/conformance; the
# generated human-readable reports below are optional views, not CI state.
test-conformance:
	go test ./tests/conformance/... -count=1
	bash tests/conformance/upstream/ossie/run.sh

semantic-correctness-coverage:
	go run ./tests/conformance/cmd/coverage

semantic-target-evidence:
	go run ./tests/conformance/cmd/evidence

# Explicit networked conformance check for the benchmark-only OKF projection.
# The external CLI is version-pinned and is not part of the default offline gate.
okf-readiness-conformance:
	bash tools/check-okf-readiness.sh

# Default correctness gate: unit + sample contracts + offline conformance.
test: test-unit test-samples test-conformance

# Deployable API/MCP smoke test.
test-e2e:
	bash tests/e2e/smoke.sh

# Real-engine execution backends. Semantic/query cases come from the shared
# tests/conformance/scenarios corpus; these packages own only physical fixtures,
# execution, and result normalization.
# DuckDB is one explicit CGO build flavor. This single gate covers the
# production Backend, runtime composition, the complete shared real-engine
# scenario corpus, and S2SBench's embedded execution fixture.
test-duckdb-backend:
	CGO_ENABLED=1 go test -tags duckdb ./execution/backend/duckdb ./cmd/metis ./cmd/s2sbench/... ./tests/engine/duckdb ./tests/benchmarks/s2sbench -count=1

test-engine-clickhouse:
	@go run ./tests/engine/datasource/cmd/require -datasource clickhouse
	go test ./tests/engine/clickhouse -count=1 -v

test-engine-clickhouse-container:
	bash tests/engine/clickhouse/run.sh

test-engine-doris:
	@go run ./tests/engine/datasource/cmd/require -datasource doris
	go test ./tests/engine/doris -count=1 -v

test-engine-doris-container:
	bash tests/engine/doris/run.sh

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || \
		(echo "gofmt required for:"; gofmt -l $$(find . -name '*.go' -not -path './vendor/*'); exit 1)

vet:
	go vet ./...

validate:
	go run ./cmd/metis validate --project finance examples/sales.ossie.yaml

# Explicit schema-upgrade maintenance target; intentionally excluded from the
# default developer and CI correctness gates.
ossie-sync:
	bash tools/update-ossie-binding.sh

# Backward-compatible aliases. New automation should use test-* targets above.
smoke: test-e2e

ossie-conformance:
	bash tests/conformance/upstream/ossie/run.sh

# Compatibility alias: ClickHouse dialect correctness now belongs to compiler
# conformance rather than a standalone dialect testcase taxonomy.
clickhouse-conformance:
	go test ./tests/conformance/compiler -count=1 -run '^TestClickHouse'

# Generic reference/reality contract. Reference implementations may contribute
# cases, but correctness is defined by canonical semantic intent and normalized
# result semantics rather than any branded implementation or generated SQL.
reference-conformance:
	go test ./tests/conformance/reference -count=1

metis-build:
	go build -o bin/metis ./cmd/metis

s2sbench-build:
	CGO_ENABLED=1 go build -tags duckdb -o bin/s2sbench ./cmd/s2sbench

s2sbench-boundary-check:
	go test ./tools/ci/s2sbenchboundary
	go run ./tools/ci/s2sbenchboundary

metis-smoke: test-samples

docker-build:
	docker build -t metis:dev .

.PHONY: licenses licenses-check

licenses:
	python3 tools/licenses.py --write

licenses-check:
	python3 tools/licenses.py --check
	bash tools/ci/license-bundle-check.sh
