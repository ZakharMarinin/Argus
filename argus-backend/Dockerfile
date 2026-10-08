FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X argus-collector/internal/version.Version=${VERSION}" -o /out/argus ./cmd/argus

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/argus ./argus

EXPOSE 8099

ENTRYPOINT ["./argus"]
