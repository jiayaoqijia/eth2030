# Build stage
FROM golang:1.27-alpine AS builder

ARG COMMIT=""
ARG VERSION=""

RUN apk add --no-cache gcc musl-dev linux-headers git

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build both binaries
COPY . .
RUN go build -tags goethkzg -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /build/bin/eth2030 ./cmd/eth2030
RUN go build -tags goethkzg -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /build/bin/eth2030-geth ./cmd/eth2030-geth

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates

COPY --from=builder /build/bin/eth2030 /usr/local/bin/
COPY --from=builder /build/bin/eth2030-geth /usr/local/bin/
RUN ln -s /usr/local/bin/eth2030 /usr/local/bin/geth

# P2P, HTTP-RPC, WS-RPC, Engine API, Metrics
EXPOSE 30303 30303/udp 8545 8546 8551 9001

ENTRYPOINT ["eth2030"]

ARG COMMIT=""
ARG VERSION=""
LABEL commit="$COMMIT" version="$VERSION"
