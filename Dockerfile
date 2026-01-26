# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o upstreamgate .

# Final stage
FROM scratch

# Copy the binary from builder
COPY --from=builder /build/upstreamgate /upstreamgate

# Expose the proxy port
EXPOSE 8090

# Run the binary
ENTRYPOINT ["/upstreamgate"]
