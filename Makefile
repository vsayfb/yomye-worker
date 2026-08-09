BINARY=cat-service
IMAGE=cat-service
COMPOSE=docker compose
TEST_COMPOSE=$(COMPOSE) -f docker-compose.test.yml

.PHONY: build run clean docker-build docker-run up down logs benchmark-ai capture-ai-baseline evaluate-ai test-e2e test-e2e-external test-infra-up test-infra-down

up:
	$(COMPOSE) up --build -d

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

build:
	go build -o bin/$(BINARY) ./cmd/worker

run:
	go run ./cmd/worker

clean:
	rm -rf bin

docker-build:
	docker build -t $(IMAGE) -f Dockerfile .

docker-run:
	docker run --rm --env-file .env.example $(IMAGE)

benchmark-ai:
	$(COMPOSE) --env-file .env --profile benchmark run --rm --build categorization-benchmark

capture-ai-baseline:
	go run ./cmd/evaluate -capture testdata/evaluation/candidate.json

evaluate-ai:
	go run ./cmd/evaluate -baseline testdata/evaluation/golden.json

test-infra-up:
	$(TEST_COMPOSE) up -d --wait postgres-test localstack-test

test-infra-down:
	$(TEST_COMPOSE) down -v --remove-orphans

# Default E2E path: isolate mutable infrastructure in disposable Docker
# containers. .env is used only for AI provider configuration and optional
# embedding settings; DB and SQS values below deliberately override it.
test-e2e:
	@set -eu; \
	$(TEST_COMPOSE) up -d --wait postgres-test localstack-test; \
	trap '$(TEST_COMPOSE) down -v --remove-orphans' EXIT INT TERM; \
	set -a; . ./.env; set +a; \
	export APP_ENV=development; \
	export DB_HOST=127.0.0.1 DB_PORT=55432 DB_USER=gigpf_test DB_PASSWORD=gigpf_test DB_NAME=gigpf_test; \
	export AWS_REGION=us-east-1 AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test; \
	export AWS_SQS_ENDPOINT=http://127.0.0.1:4567; \
	export CATEGORIZATION_SQS_URL=http://127.0.0.1:4567/000000000000/categorization-e2e; \
	export NOTIFICATION_SQS_URL=http://127.0.0.1:4567/000000000000/notification-e2e; \
	export LOCAL_OLLAMA_ENDPOINT="$${E2E_OLLAMA_ENDPOINT:-http://127.0.0.1:11434}"; \
	RUN_E2E_TESTS=1 go test ./internal/processor/... -run E2E -v

# Explicit escape hatch for validating against an intentionally configured
# external/shared environment. Unlike test-e2e, this trusts every value in .env.
test-e2e-external:
	set -a; . ./.env; set +a; RUN_E2E_TESTS=1 go test ./internal/processor/... -run E2E -v
