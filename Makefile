BINARY := bin/truenas-leds
GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: test lint fix fmt tidy build

test:
	go test ./...

lint:
	go vet ./...

fix: fmt tidy

fmt:
	gofmt -w $(GOFILES)

tidy:
	go mod tidy

build:
	mkdir -p $(dir $(BINARY))
	go build -o $(BINARY) .