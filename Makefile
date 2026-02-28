.PHONY: build test lint clean fmt vet all

BINARY=agentid
MODULE=github.com/samudary/agentid

build:
	go build -o bin/$(BINARY) ./cmd/agentid

test:
	go test ./... -v -race

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

fmt:
	gofmt -s -w .

vet:
	go vet ./...

all: fmt vet lint test build
