.PHONY: build install test

build:
	go build -o bin/wiki ./cmd/wiki

install:
	go install ./cmd/wiki

test:
	go test ./...
