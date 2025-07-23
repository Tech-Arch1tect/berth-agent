FROM golang:1.24.4-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -o berth-agent \
    ./cmd/berth-agent

FROM alpine:3

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    docker-cli \
    docker-compose \
    && rm -rf /var/cache/apk/*

WORKDIR /app

COPY --from=builder /app/berth-agent .

RUN mkdir -p /opt/compose

CMD ["./berth-agent"]