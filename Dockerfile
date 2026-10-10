# Build
FROM golang:1.27-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /restflow-api ./cmd/api

# Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 1000 restflow
USER restflow
WORKDIR /app
COPY --from=build /restflow-api /app/restflow-api
ENV PORT=3000
EXPOSE 3000
# Render uses HTTP health check path; this is for local/docker compose
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT:-3000}/health/ready" || exit 1
ENTRYPOINT ["/app/restflow-api"]
