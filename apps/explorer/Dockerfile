# Stage 1: Build the Explorer
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY . .

WORKDIR /app/apps/explorer
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /orpscan main.go indexer.go db.go api.go

# Stage 2: Minimal image
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /orpscan .

EXPOSE 5000
CMD ["./orpscan"]
