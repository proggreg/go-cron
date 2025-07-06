# Build stage
FROM golang:1.22 as builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Install Delve
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# Build the app with debug flags
RUN go build -gcflags="all=-N -l" -o main .

# Final image
FROM golang:1.22

WORKDIR /app

# Copy the built binary and Delve
COPY --from=builder /app/main .
COPY --from=builder /go/bin/dlv /go/bin/dlv

# Copy static/frontend and swagger-ui directories
COPY --from=builder /app/frontend ./frontend
COPY --from=builder /app/swagger-ui ./swagger-ui
COPY --from=builder /app/swagger.yaml ./

# Expose app and Delve ports
EXPOSE 8080 40000

# Start the app with Delve in headless mode
CMD ["/go/bin/dlv", "exec", "./main", "--headless", "--listen=:40000", "--api-version=2", "--accept-multiclient", "--log"]