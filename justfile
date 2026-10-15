set dotenv-load

up:
    docker compose up -d

down:
    docker compose down

test:
    go test ./...

# Calls the real Gemini and Workers AI APIs (metered).
test-live:
    KINTSUGI_LIVE=1 go test ./internal/model/ -run 'Gemini|ClefAgrees' -v

scenario:
    go run ./cmd/kintsugi scenario clean
    go run ./cmd/kintsugi scenario demo
