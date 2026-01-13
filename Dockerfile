FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build \
    -ldflags='-w -s -extldflags "-static"' \
    -o berth-agent \
    .

FROM docker.io/techarchitect/berth-agent-base:latest

RUN chmod +x /usr/bin/docker-compose

COPY --from=builder /app/berth-agent ./berth-agent

EXPOSE 8080