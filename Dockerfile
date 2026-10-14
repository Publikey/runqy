# Build from the repository root:  docker build -t runqy .

# UI stage: build the monitoring dashboard so the image never depends on the committed
# app/monitoring/ui/build output being up to date.
FROM node:20-alpine AS ui

WORKDIR /ui
COPY app/monitoring/ui/package.json app/monitoring/ui/package-lock.json ./
RUN npm ci
COPY app/monitoring/ui/ .
RUN npm run build

# Build stage
FROM golang:1.24-alpine AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /build

# Copy go mod files first for better caching
COPY app/go.mod app/go.sum ./
RUN go mod download

# Copy source code, then the freshly built dashboard (embedded by monitoring/static.go)
COPY app/ .
RUN rm -rf monitoring/ui/build
COPY --from=ui /ui/build monitoring/ui/build

# Build with version info
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X github.com/Publikey/runqy/cmd.Version=${VERSION} -X github.com/Publikey/runqy/cmd.GitCommit=${COMMIT} -X github.com/Publikey/runqy/cmd.BuildDate=${BUILD_DATE}" \
    -o /runqy .

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -h /app runqy
USER runqy
WORKDIR /app

# Copy binary from builder
COPY --from=builder /runqy /usr/local/bin/runqy

# Queue YAML definitions (optional, can be mounted over)
COPY --chown=runqy:runqy deployment/ /app/deployment/
ENV QUEUE_WORKERS_DIR=/app/deployment

EXPOSE 3000

ENTRYPOINT ["runqy"]
CMD ["serve"]
