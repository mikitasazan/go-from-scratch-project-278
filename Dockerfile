# 1) Build frontend
FROM node:24-alpine AS frontend-builder
WORKDIR /build/frontend

COPY package*.json ./

RUN --mount=type=cache,target=/root/.npm \
    npm ci --prefer-offline --no-audit

# 2) Build backend
FROM golang:1.26-alpine AS backend-builder
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

# GOARCH не задаём: образ собирается под платформу, которую выбрал Docker, и
# жёсткий amd64 не запустился бы в arm64-образе на этой машине.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o /build/app .

# 3) Runtime
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata bash caddy

WORKDIR /app

COPY --from=backend-builder /build/app /app/bin/app
COPY --from=frontend-builder \
    /build/frontend/node_modules/@hexlet/project-url-shortener-frontend/dist \
    /app/public

COPY --from=backend-builder /build/code/db/migrations /app/db/migrations
COPY --from=backend-builder /build/goose /usr/local/bin/goose

COPY bin/run.sh /app/bin/run.sh
RUN chmod +x /app/bin/run.sh

COPY Caddyfile /etc/caddy/Caddyfile

EXPOSE 80

CMD ["/app/bin/run.sh"]
