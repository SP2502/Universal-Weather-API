# Build stage
FROM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway ./cmd/gateway && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /uapctl ./cmd/uapctl

# Run stage
FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

WORKDIR /home/appuser
COPY --from=builder /gateway /usr/local/bin/gateway
COPY --from=builder /uapctl /usr/local/bin/uapctl

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/gateway"]
