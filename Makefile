.PHONY: build run test clean deploy register-gmail

build:
	go build -o bin/receiver ./cmd/receiver

run: build
	./bin/receiver

test:
	go test ./...

clean:
	rm -rf bin/

# One-time: register Gmail push notifications
register-gmail:
	go run ./scripts/register-push/main.go

# Deploy to Fly.io
deploy:
	fly deploy

# Set Fly.io secrets
fly-secrets:
	fly secrets set \
		ANTHROPIC_API_KEY="$(ANTHROPIC_API_KEY)" \
		NOTION_API_KEY="$(NOTION_API_KEY)" \
		VERCEL_TOKEN="$(VERCEL_TOKEN)" \
		GITHUB_TOKEN="$(GITHUB_TOKEN)" \
		PUBSUB_VERIFY_TOKEN="$(PUBSUB_VERIFY_TOKEN)"

# Local dev with config
dev: build
	ANTHROPIC_API_KEY=$(ANTHROPIC_API_KEY) ./bin/receiver

tidy:
	go mod tidy
