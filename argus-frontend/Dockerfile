FROM golang:1.27.2-alpine3.24@sha256:85dc1069ac644ea3c527b177303a406eb3358192816cd7f9e5848eb658851673 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X argus-frontend/internal/app.version=${VERSION}" -o /out/argus-web ./cmd/web

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

WORKDIR /app

COPY --from=builder /out/argus-web ./argus-web
COPY config.example.yaml ./config.yaml

EXPOSE 8088

ENTRYPOINT ["./argus-web"]
