.PHONY: all web server agent test run dev clean

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

run: server
	./bin/searchboard -config configs/searchboard.yaml

dev:
	@echo "terminal 1: go run ./cmd/searchboard   terminal 2: cd web && npm run dev"

clean:
	rm -rf bin data web/dist agent-rs/target
