.PHONY: fmt lint test race build validate check run client conformance docker

fmt:
	gofmt -w ./cmd ./internal ./pkg

lint:
	@test -z "$$(gofmt -l ./cmd ./internal ./pkg)" || \
	  (echo "Go files require gofmt"; gofmt -l ./cmd ./internal ./pkg; exit 1)
	go vet ./...

# Unit and integration tests.
test:
	go test ./...

# Race detection is a release gate because idempotency and task stores are
# explicitly concurrency-sensitive.
race:
	go test -race ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/ascp-server ./cmd/ascp-server
	CGO_ENABLED=0 go build -trimpath -o bin/ascp-client ./cmd/ascp-client
	CGO_ENABLED=0 go build -trimpath -o bin/ascp-conformance ./cmd/ascp-conformance

validate:
	python ./scripts/validate_specs.py

check: lint race build validate

run:
	go run ./cmd/ascp-server

client:
	go run ./cmd/ascp-client

conformance:
	go run ./cmd/ascp-conformance

docker:
	docker build -t ascp-reference:0.2 .
