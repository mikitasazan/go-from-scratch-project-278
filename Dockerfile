# Build backend
FROM golang:1.27-alpine AS backend-builder
RUN apk add --no-cache git
WORKDIR /build/code

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Версия goose берётся из go.mod, где он объявлен директивой tool, а не из
# `@latest`: иначе образ соберётся с другой версией, чем у вас локально.
RUN --mount=type=cache,target=/go/pkg/mod \
    go build -o /build/goose github.com/pressly/goose/v3/cmd/goose

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /build/app .

# Runtime
FROM alpine:3.22

# run.sh is a bash script (#!/usr/bin/env bash), and alpine ships without bash.
RUN apk add --no-cache bash

WORKDIR /app

COPY --from=backend-builder /build/app /app/bin/app

COPY --from=backend-builder /build/code/db/migrations /app/db/migrations
COPY --from=backend-builder /build/goose /usr/local/bin/goose

COPY bin/run.sh /app/bin/run.sh
RUN chmod +x /app/bin/run.sh

EXPOSE 8080

CMD ["/app/bin/run.sh"]
