FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X argus-frontend/internal/app.version=${VERSION}" -o /out/argus-web ./cmd/web

FROM alpine:3.20

WORKDIR /app

COPY --from=builder /out/argus-web ./argus-web
COPY config.example.yaml ./config.yaml

EXPOSE 8088

ENTRYPOINT ["./argus-web"]
