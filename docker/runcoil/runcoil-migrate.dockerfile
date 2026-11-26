FROM golang:1.27 AS builder

WORKDIR /app
COPY go.mod go.sum ./
COPY proto/go.mod proto/go.sum ./proto/

RUN go mod download

COPY . .
RUN GOWORK=off \
    CGO_ENABLED=0 \
    GOOS=linux \
    go build -ldflags "-s -w" \
    -o main ./cmd/runcoil-migrate


FROM alpine:3.24 AS prod

WORKDIR /app

RUN addgroup -S appuser && adduser -S appuser -G appuser

COPY --chown=appuser:appuser --from=builder /app/main /app/main

USER appuser

ENTRYPOINT ["/app/main"]
