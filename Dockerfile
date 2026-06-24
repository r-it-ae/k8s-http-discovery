# Stage 1: Builder
FROM golang:1.23-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN go build -o /app ./cmd/

# Stage 2: Runtime
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app /app

USER nonroot

ENTRYPOINT ["/app"]
