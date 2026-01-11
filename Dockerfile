FROM golang:1.25.0-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o berth-agent \
    .

FROM alpine:3

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    docker-cli \
    docker-compose \
    curl \
    && rm -rf /var/cache/apk/*

# Install grype
RUN curl -sSfL https://get.anchore.io/grype | sh -s -- -b /usr/local/bin

WORKDIR /app

COPY --from=builder /app/berth-agent .

RUN mkdir -p /opt/compose

EXPOSE 8080

CMD ["./berth-agent"]