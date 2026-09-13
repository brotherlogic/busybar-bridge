# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /app

# Download Go dependencies with layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source tree and compile statically linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /busybar-bridge .

# Minimal runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /busybar-bridge /busybar-bridge

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/busybar-bridge"]
