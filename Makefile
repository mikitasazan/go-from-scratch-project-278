build:
	go build -o bin/link-shortener .

run:
	go run .

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...

test:
	go test ./...

.PHONY: build run lint lint-fix test
