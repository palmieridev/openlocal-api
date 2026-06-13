.PHONY: test sqlc migrate-up migrate-down run

DATABASE_URL ?= postgres://openlocal:openlocal@localhost:5432/openlocal?sslmode=disable

test:
	go test ./...

sqlc:
	sqlc generate

migrate-up:
	migrate -path db/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path db/migrations -database "$(DATABASE_URL)" down 1

run:
	go run ./cmd/api

