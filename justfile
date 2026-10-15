set dotenv-load

up:
    docker compose up -d

down:
    docker compose down

# Format, vet and build. Run before every push.
check:
    test -z "$(gofmt -l .)" || (gofmt -l . && echo 'run gofmt -w .' && exit 1)
    go vet ./...
    go build ./...

test:
    go test ./...

# Calls the real Gemini and Workers AI APIs (metered).
test-live:
    KINTSUGI_LIVE=1 go test ./internal/model/ -run 'Gemini|ClefAgrees' -v

scenario:
    go run ./cmd/kintsugi scenario clean
    go run ./cmd/kintsugi scenario demo
