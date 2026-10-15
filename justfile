set dotenv-load

up:
    docker compose up -d

down:
    docker compose down

test:
    go test ./...

scenario:
    go run ./cmd/kintsugi scenario clean
    go run ./cmd/kintsugi scenario demo
