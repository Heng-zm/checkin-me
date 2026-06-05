run:
	go run ./cmd/api

fmt:
	gofmt -w ./cmd ./internal

tidy:
	go mod tidy

test:
	go test ./...

build:
	go build -o bin/checkinme-api ./cmd/api

db-up:
	docker compose up -d postgres

api-up:
	docker compose --profile api up -d --build

db-down:
	docker compose down
