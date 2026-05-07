# orbit-os-sdk-go

Go module with **generated Protocol Buffers / gRPC** definitions and a **high-level client** for talking to OrbitOS services (Unix socket or TCP).

**Repository:** https://github.com/OrbitOS-org/orbit-os-sdk-go

## Requirements

- Go **1.25+** (see `go.mod`)

## Use as a dependency

Major version **26** uses a `/v26` suffix (Go module versioning):

```go
import (
    "github.com/OrbitOS-org/orbit-os-sdk-go/v26/client"
)
```

After you tag a release on GitHub (e.g. `v26.0.0`):

```go
require github.com/OrbitOS-org/orbit-os-sdk-go/v26 v26.0.0
```

To pin a **branch tip** (e.g. `API_26`):

```bash
go get github.com/OrbitOS-org/orbit-os-sdk-go/v26@API_26
```

For **local development** next to your app, use a `replace` (path relative to your app’s `go.mod`):

```go
require github.com/OrbitOS-org/orbit-os-sdk-go/v26 v0.0.0

replace github.com/OrbitOS-org/orbit-os-sdk-go/v26 => ../orbit-os-sdk-go
```

## Layout

| Path | Purpose |
|------|---------|
| `api/` | Generated `*.pb.go` / `*_grpc.pb.go` per service |
| `client/` | gRPC client helpers (`NewUDSClient`, `NewTCPClient`, `NewClientAuto`, service managers) |
| `logger/` | Optional structured logging helpers for device apps |
| `api/proto/gen.sh` | Regenerates Go from protos (expects `gravity-api-proto` sibling to this SDK tree) |
| `scripts/build_package.sh` | Builds an ORB from a Go main (run from **orbit-os-workspace-go** root, or set `ORBIT_PROJECT_ROOT`) |

## ORB / device packages

From the **app** repository that contains `cmd/examples` (e.g. `orbit-os-workspace-go` with this module as `orbit-os-sdk-go/`):

```bash
./orbit-os-sdk-go/scripts/build_package.sh
./orbit-os-sdk-go/scripts/build_package.sh -path basic -arch arm64
```

If the SDK is not checked out under the app repo, set `ORBIT_PROJECT_ROOT` to that app’s root directory.

## Regenerating code

From `api/proto/`:

```bash
./gen.sh
```

Set `GO_IMPORT_PREFIX` in `gen.sh` if the Go module path changes; it must match `go.mod`.

## Build

```bash
go build ./...
```

## License

See the repository default or add a `LICENSE` file in this repo if needed.
