# CodeCell Runner

CodeCell Runner is a Go-based gRPC service that builds and executes user-provided source code inside language-specific Docker containers. For each request, it prepares an isolated workspace, generates the necessary project files, mounts the workspace into a container, and streams stdout/stderr back to the client alongside the final exit code.

## Prerequisites

- Go toolchain (Go 1.22+ recommended).
- Docker Engine with permissions to build and run containers.
- `protoc` with Go and gRPC plugins if you regenerate stubs.
- Optional: tooling such as `buf` or `make` if you add helper scripts.

## Initial Setup

1. Build the required language images. Example for .NET:
   - `docker build -f images/dotnet.Dockerfile -t codecell/dotnet .`
2. Download Go module dependencies:
   - `go mod download`
3. Generate gRPC stubs if you modify `protocol/runner.proto`:
   - `protoc --go_out=generated --go-grpc_out=generated protocol/runner.proto`

## gRPC API

- Service: `RunnerService` (package `runner.v1`).
- Methods:
  - `Run(RunRequest) -> stream RunResponseMessage` (fields: `source_code`, `language`, `timeout_seconds`, `stdin`).
  - `Stop(StopRequest) -> StopResponse`.
