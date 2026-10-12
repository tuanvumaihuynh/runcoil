FROM golang:1.26 AS builder

WORKDIR /app
COPY go.mod go.sum ./

RUN go mod download

COPY . .
RUN CGO_ENABLED=0 \
    GOOS=linux \
    go build -ldflags "-s -w" \
    -o main ./cmd/migrate


FROM alpine:3.23 AS prod

WORKDIR /app

RUN addgroup -S appuser && adduser -S appuser -G appuser

COPY --chown=appuser:appuser --from=builder /app/main /app/main

USER appuser

ENTRYPOINT ["/app/main"]
