.PHONY: build install install-skill test

build:
	go build -o bin/wiki ./cmd/wiki

install:
	go install ./cmd/wiki

# Install the binary, then link the skill, write the config and register the hook.
install-skill: install
	go run ./cmd/wiki install --repo $(CURDIR)

test:
	go test ./...
