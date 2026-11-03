.PHONY: test vet fmt check build up down logs

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

# Fails if any file needs formatting (this is what CI runs).
fmt:
	@test -z "$$(gofmt -l .)" || (echo "needs gofmt:"; gofmt -l .; exit 1)

check: fmt vet test

build:
	go build -o bin/api .

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f api
