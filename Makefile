.PHONY: all web server agent test fmt check run dev clean

all: web server agent

web:
	cd web && npm install && npm run build

server:
	@test -f web/dist/index.html || { mkdir -p web/dist; echo "<!doctype html><title>Searchboard</title>" > web/dist/index.html; }
	go build -o bin/searchboard ./cmd/searchboard

agent:
	cd agent-rs && cargo build --release

test:
	go test ./...
	cd agent-rs && cargo test

fmt:
	gofmt -w .
	cd agent-rs && cargo fmt

# Runs the same checks as CI.
check:
	scripts/check-english.sh
	scripts/check-pr-title_test.sh
	@test -f web/dist/index.html || { mkdir -p web/dist; echo "<!doctype html><title>Searchboard</title>" > web/dist/index.html; }
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test -race -count=1 ./...
	cd agent-rs && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test
	cd web && npm run typecheck && npm run build

run: server
	./bin/searchboard -config configs/searchboard.yaml

dev:
	@echo "terminal 1: go run ./cmd/searchboard"
	@echo "terminal 2: cd web && npm run dev"

clean:
	rm -rf bin data web/dist agent-rs/target
