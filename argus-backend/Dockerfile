FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/argus ./cmd/argus

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/argus ./argus
COPY config.yaml ./config.yaml
COPY migrations ./migrations

EXPOSE 8099

ENTRYPOINT ["./argus"]
