# Stage 1: Build the Node
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy the entire workspace (assumes docker build context is the openreserve root)
COPY . .

# Build the core node binary
WORKDIR /app/node
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /openreserve-node main.go

# Stage 2: Create a minimal production image
FROM alpine:latest

# Install basic networking tools for debugging inside the container
RUN apk add --no-cache bash curl

WORKDIR /root/

# Copy the compiled binary from the builder stage
COPY --from=builder /openreserve-node .

# Expose P2P port (3000) and REST API port (8080)
EXPOSE 3000
EXPOSE 8080

# Run the node
CMD ["./openreserve-node"]
