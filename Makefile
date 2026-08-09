BINARY=cat-service
IMAGE=cat-service
COMPOSE=docker compose

.PHONY: build run clean docker-build docker-run up down logs benchmark-ai capture-ai-baseline evaluate-ai test-e2e

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

test-e2e:
	set -a; . ./.env; set +a; RUN_E2E_TESTS=1 go test ./internal/processor/... -run E2E -v
