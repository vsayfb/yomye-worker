BINARY=cat-service
IMAGE=cat-service
COMPOSE=docker compose

.PHONY: build run clean docker-build docker-run up down logs

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
