SHELL := /bin/bash

.PHONY: test test-integration vet fmt check build up down logs seed

test:
	go test -race -count=1 ./...

# Runs everything, including the Postgres and Redis integration tests, against
# the compose services (start them first: docker compose up -d postgres-db redis-cache).
test-integration:
	@set -a; . ./.env; set +a; \
	TEST_DATABASE_URL="postgres://campus:$$POSTGRES_PASSWORD@localhost:5432/campus_db?sslmode=disable" \
	TEST_REDIS_ADDR=localhost:6379 \
	go test -race -count=1 ./...

# Fills a running API with sample students: make seed N=120
seed:
	./scripts/seed.sh $(or $(N),60)

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
