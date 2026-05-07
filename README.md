# FluxSolar

Real-time solar monitoring application for [OrbitOS](https://www.orbit-os.org) IoT devices. Reads data from solar inverters via Modbus RTU over RS485/UART and exposes a live web dashboard with historical charts, MQTT integration, and Home Assistant auto-discovery.

---

## Features

- **Live dashboard** — animated energy flow diagram showing PV generation, battery state, grid import/export, and load consumption
- **Historical analytics** — per-minute SQLite storage with daily, monthly, and yearly aggregated charts
- **Multi-inverter support** — profile-based configuration (Swatten, Deye, and custom JSON profiles)
- **MQTT / Home Assistant** — publishes all readings to an MQTT broker with auto-discovery payloads
- **Server-Sent Events** — instant push to all connected browsers, no polling required
- **Automatic UART watchdog** — reopens the serial port after 5 consecutive read errors

---

## Architecture

```
OrbitOS Device
├── UART (RS485) ──► Inverter (Modbus RTU)
│
└── FluxSolar binary
    ├── internal/solar      — poll cycle, register decoding
    ├── internal/modbus     — Modbus RTU over UART
    ├── internal/profile    — inverter profile loader (JSON)
    ├── internal/history    — SQLite time-series store
    └── internal/mqttclient — MQTT publish + HA discovery
        │
        └── HTTP server (default :8080)
            ├── /           — live dashboard
            ├── /mqtt        — MQTT configuration page
            ├── /events      — SSE stream (live push)
            └── /api/*       — JSON REST API
```

---

## Getting Started

### Prerequisites

- [OrbitOS SDK v26](https://www.orbit-os.org/sdk/api-reference.html) (`./orbit-os-sdk-go` included)
- Go 1.25.4+
- Orbit Studio VS Code extension (or `orbit-deploy` CLI)
- OrbitOS device with RS485/UART port connected to a compatible solar inverter

### Build & Deploy with the Orbit Studio Extension

The recommended workflow uses the **Orbit Studio** VS Code extension.

#### 1. Configure the device address

Open `orbit.project.json` and set `deviceHost` to your OrbitOS device IP:

```json
{
  "deviceHost": "192.168.5.226",
  "appPath": "cmd/FluxSolar"
}
```

#### 2. Build the .orb package

Open the **Orbit** panel in the VS Code sidebar (rocket icon), then click **Build ORB** — or run the task manually:

```
Terminal → Run Task → Orbit: Build ORB
```

This invokes `buildOrbCli.js` from the extension, compiles the Go binary for the target architecture, and produces:

```
.orbit/fluxsolar_v0.1.0.orb
```

The `.orb` file is a self-contained application bundle (binary + web assets + profiles + icon + manifest).

#### 3. Deploy to the device

In the **Orbit** sidebar click **Deploy to device** — or:

```
Terminal → Run Task → Orbit: Deploy to device
```

Under the hood this runs:

```bash
go run ./orbit-os-sdk-go/cmd/orbit-deploy -root .
```

The deploy tool connects to the device at `deviceHost`, uploads the `.orb` package, installs it, and restarts the app. The dashboard becomes available at:

```
http://<deviceHost>:8080
```

#### 4. First-run setup

1. Open the dashboard in a browser.
2. The app auto-detects the UART port and loads the default **Swatten** profile.
3. If your inverter is a different model, go to **Settings → Profile** and select or import the correct profile.
4. (Optional) Go to `/mqtt` to configure Home Assistant integration.

#### Building from the terminal only

```bash
# build binary locally (host architecture)
go build ./cmd/FluxSolar

# build UART diagnostic tool
go build ./cmd/uart-test

# sync workspace dependencies after SDK changes
go work sync && go mod tidy
```

### Configuration

Edit `orbit.project.json` to set your device address and build paths:

```json
{
  "deviceHost": "192.168.5.226",
  "appPath": "cmd/FluxSolar"
}
```

The app accepts CLI flags (useful for local development):

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `""` | OrbitOS device IP |
| `--uart` | `""` | UART port (auto-detected) |
| `--profile` | `swatten` | Inverter profile ID |
| `--profiles-dir` | `""` | Custom profiles directory |
| `--http-port` | `8080` | Dashboard web server port |
| `--dev` | `false` | Enable Profile Manager UI |

---

## Inverter Profiles

Profiles are JSON files that describe the UART settings, Modbus slave address, poll interval, and full register map for a specific inverter model. Built-in profiles live in `cmd/FluxSolar/orb/data/`.

```json
{
  "id": "swatten",
  "name": "Swatten Inverter",
  "uart": { "baud": 9600, "parity": "N", "stopBits": 1 },
  "modbus": { "slave": 1, "pollIntervalSec": 5 },
  "registers": [
    {
      "id": "pv_power",
      "address": 32064,
      "functionCode": 4,
      "type": "uint16",
      "unit": "W",
      "multiplier": 1
    }
  ]
}
```

Custom profiles can be created via the Profile Manager (run with `--dev`).

---

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/data` | Latest inverter snapshot |
| GET | `/api/history?range=day\|month\|year` | Time-series buckets for charts |
| GET | `/api/totals?period=day\|month\|year` | Cumulative energy totals |
| GET/POST | `/api/profile` | Get or set active profile |
| GET | `/api/profiles` | List available profiles (dev mode) |
| GET | `/api/ports` | List UART ports |
| GET/POST | `/api/mqtt` | Get or set MQTT configuration |
| GET | `/api/version` | App version |
| GET | `/events` | SSE stream — live data push |

---

## MQTT / Home Assistant

Go to `/mqtt` in the browser to configure the MQTT broker. Once connected, FluxSolar publishes to `fluxsolar/<reading_id>` and sends Home Assistant MQTT Discovery payloads so all sensors appear automatically in HA.

---

## Project Layout

```
FluxSolar/
├── cmd/
│   ├── FluxSolar/          — main application
│   │   ├── main.go         — HTTP server, orchestration
│   │   ├── web/            — dashboard HTML/CSS/JS
│   │   └── orb/data/       — built-in inverter profiles
│   └── uart-test/          — UART/Modbus diagnostic tool
├── internal/
│   ├── solar/              — poller & register decoder
│   ├── modbus/             — Modbus RTU client
│   ├── profile/            — profile schema & loader
│   ├── history/            — SQLite time-series store
│   └── mqttclient/         — MQTT + Home Assistant discovery
├── orbit-os-sdk-go/        — OrbitOS SDK v26 (local copy)
├── go.mod / go.work
└── orbit.project.json      — build & deploy config
```

---

## Development Notes

- **TLS certs** — dev files go in `cmd/certs/grpc/`.
- **Launcher icon** — must stay at `cmd/FluxSolar/orb/icon.svg`.
- **SDK squiggles** — if VS Code shows red squiggles in `go.mod`, run `go work sync && go mod tidy` in the project root and in `./orbit-os-sdk-go`, then reload the window.
- **App identity** — module `fluxsolar` · on-device ID `org.orbit-os.apps.fluxsolar` · app folder `cmd/FluxSolar`.
- **SDK API reference** — https://www.orbit-os.org/sdk/api-reference.html
- **License** — [GPL-3.0](LICENSE)
