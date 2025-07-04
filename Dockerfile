# Build stage
FROM golang:1.23.7 AS builder

WORKDIR /app



# Copy the source code
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the application
RUN CGO_ENABLED=0 go build -gcflags="all=-N -l"  -o /app/url-shortener .

# Final stage
FROM alpine:3.22.0

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/url-shortener .
# Copy the templates
COPY templates/ templates/

EXPOSE 8080

# Run the application
ENTRYPOINT ["/app/url-shortener"]