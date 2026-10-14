# Build stage
FROM docker.io/library/golang:1.25.1 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Build with CGO disabled for static binary as musl is used by alpine image
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o backend .

# Test stage (CI only): docker build --target test .
FROM builder AS test
# Pin pgFormatter so formatting is reproducible everywhere
RUN apt-get update && apt-get install -y --no-install-recommends pgformatter=5.6-* \
    && rm -rf /var/lib/apt/lists/*

# Runtime stage (default target)
FROM docker.io/alpine:latest

RUN apk --no-cache add ca-certificates postgresql-client

WORKDIR /root/
COPY --from=builder /app/backend .

EXPOSE 8004
ENTRYPOINT ["./backend"]
