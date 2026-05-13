.PHONY: test vet build docker-build

test:
	cd backend && go test ./...

vet:
	cd backend && go vet ./...

build:
	cd backend && go build -o main ./cmd/server

docker-build:
	docker compose build backend
