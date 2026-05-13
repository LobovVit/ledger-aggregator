# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod ./
COPY go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application
ARG GIT_COMMIT=unknown
ARG GIT_BRANCH=unknown
RUN go build -ldflags "-X 'svap-query-service/backend/internal/version.GitCommit=${GIT_COMMIT}' -X 'svap-query-service/backend/internal/version.GitBranch=${GIT_BRANCH}'" -o main ./cmd/server

# Final stage
FROM alpine:3.22

RUN apk add --no-cache ca-certificates

WORKDIR /root/

# Copy the pre-built binary
COPY --from=builder /app/main .
COPY --from=builder /app/db/migrations ./db/migrations
COPY .env.example .env

# Expose port
EXPOSE 8080

# Command to run the executable
CMD ["./main"]
