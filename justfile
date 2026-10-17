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
    cd web && pnpm exec vp check

test:
    go test ./...

# Calls the real Gemini and Workers AI APIs (metered).
test-live:
    KINTSUGI_LIVE=1 go test ./internal/model/ -run 'Gemini|ClefAgrees' -v

scenario:
    go run ./cmd/kintsugi scenario clean
    go run ./cmd/kintsugi scenario demo

# Clean run, poisoned run, repair, then check the repair matches the clean run.
demo:
    go run ./cmd/kintsugi scenario clean
    go run ./cmd/kintsugi scenario demo
    go run ./cmd/kintsugi repair demo partner-blog/policy-update
    go run ./cmd/kintsugi diff clean demo

serve:
    go run ./cmd/kintsugi serve

# Dashboard dev server on :5173. Run `just serve` alongside it.
web:
    cd web && pnpm exec vp dev

web-test:
    cd web && pnpm exec vp test

# Build the dashboard against the hosted API and deploy it to Cloudflare.
# CF_API_TOKEN is the Workers AI token; unset it so Void uses the deploy login.
deploy-web api:
    cd web && VITE_API_URL={{api}} pnpm exec vp build
    cd web && env -u CF_API_TOKEN pnpm exec void deploy --platform cloudflare --dir dist

# Copy the local model response cache to the hosted FalkorDB in .env.cloud.local.
seed-cache:
    go run ./cmd/kintsugi cache export | (set -a && . ./.env.cloud.local && go run ./cmd/kintsugi cache import)
