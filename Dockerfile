FROM golang:1.26.4-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o bin/cat-worker ./cmd/worker


FROM alpine:3.21

WORKDIR /app

COPY --from=builder /app/bin/cat-worker .

EXPOSE 8082

CMD ["./cat-worker"]