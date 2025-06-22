# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install git (required for go mod) and build tools
RUN apk add --no-cache git

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the Go app (static binary)
RUN CGO_ENABLED=0 GOOS=linux go build -o go-cron main.go

# Final stage
FROM alpine:latest

WORKDIR /app

# Copy the built Go binary
COPY --from=builder /app/go-cron .

# Copy static assets (frontend and swagger-ui)
COPY frontend ./frontend
COPY swagger-ui ./swagger-ui
COPY swagger.yaml ./

# Expose the port the app runs on
EXPOSE 8080

# Run the Go app
CMD ["./go-cron"] 