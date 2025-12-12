# Builder stage
FROM golang:1.25-alpine AS builder

ARG PROTOC_GEN_GO_VERSION=1.36.11
ARG PROTOC_GEN_GO_GRPC_VERSION=1.6.0

RUN apk add --no-cache ca-certificates git tzdata build-base upx protobuf-dev

ENV GOBIN=/usr/local/bin \
    PATH=/usr/local/bin:${PATH}
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v${PROTOC_GEN_GO_VERSION} && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${PROTOC_GEN_GO_GRPC_VERSION}

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p generated

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    protoc --proto_path=protocol \
      --go_out=generated \
      --go_opt=paths=source_relative \
      --go-grpc_out=generated \
      --go-grpc_opt=paths=source_relative \
      protocol/runner.proto

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -ldflags "-s -w" -trimpath -o /out/codecell-runner ./cmd && \
    upx --lzma --best /out/codecell-runner

# Final minimal image using distroless static
FROM gcr.io/distroless/static:nonroot

WORKDIR /app
COPY --from=builder /out/codecell-runner /app/codecell-runner

USER nonroot:nonroot
ENTRYPOINT ["/app/codecell-runner"]
