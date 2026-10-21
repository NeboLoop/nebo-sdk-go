# Nebo Go SDK

Official Go SDK for building [Nebo](https://neboloop.com) apps.

## Install

```bash
go get github.com/neboloop/nebo-sdk-go
```

## Quick Start

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"

    nebo "github.com/neboloop/nebo-sdk-go"
)

type Calculator struct{}

func (c *Calculator) Name() string        { return "calculator" }
func (c *Calculator) Description() string { return "Performs arithmetic calculations." }

func (c *Calculator) Schema() json.RawMessage {
    return nebo.NewSchema("add", "subtract", "multiply", "divide").
        Number("a", "First operand", true).
        Number("b", "Second operand", true).
        Build()
}

func (c *Calculator) Execute(_ context.Context, input json.RawMessage) (string, error) {
    var in struct {
        Action string  `json:"action"`
        A      float64 `json:"a"`
        B      float64 `json:"b"`
    }
    if err := json.Unmarshal(input, &in); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    switch in.Action {
    case "add":
        return fmt.Sprintf("%g", in.A+in.B), nil
    case "subtract":
        return fmt.Sprintf("%g", in.A-in.B), nil
    case "multiply":
        return fmt.Sprintf("%g", in.A*in.B), nil
    case "divide":
        if in.B == 0 {
            return "", fmt.Errorf("division by zero")
        }
        return fmt.Sprintf("%g", in.A/in.B), nil
    default:
        return "", fmt.Errorf("unknown action: %s", in.Action)
    }
}

func main() {
    app, err := nebo.New()
    if err != nil {
        log.Fatal(err)
    }
    app.RegisterTool(&Calculator{})
    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

## How Your App Runs

Nebo launches your app binary and talks to it over gRPC. `app.Run()` does all of this for you:

1. Listens on a free loopback port (`127.0.0.1:0`).
2. Prints one line to stdout, `nebo-app-listening 127.0.0.1:<port>`, so Nebo knows where to connect. Write your own logs to stderr or stdout after that; they go to the app's `sidecar.log`.
3. Refuses every call that does not carry `authorization: Bearer <NEBO_SIDECAR_TOKEN>`, Nebo's health probe included.
4. Returns `nil` when stdin closes (Nebo is gone) or on SIGTERM/SIGINT, so your app exits cleanly.

## Environment

Nebo clears the environment and sets these. Read them with `app.Env()`.

| Variable | `AppEnv` field | Meaning |
|----------|----------------|---------|
| `NEBO_APP_ID` | `ID` | App ID from the manifest |
| `NEBO_APP_NAME` | `Name` | App name |
| `NEBO_APP_VERSION` | `Version` | App version |
| `NEBO_APP_DIR` | `Dir` | The app's (versioned) code folder |
| `NEBO_DATA_DIR` | `DataDir` | The app's persistent data folder; also the working directory |
| `NEBO_API_URL` | `APIURL` | Nebo's local API, `http://127.0.0.1:<port>` |
| `NEBO_APP_TOKEN` | `Token` | Outbound token: send it when your app calls `NEBO_API_URL`. Nebo never sends it to your app |
| `NEBO_SIDECAR_TOKEN` | (none) | Inbound token: every call from Nebo carries it. `nebo.New()` requires it, checks it for you and removes it from the environment |

Nebo gives each launch two different tokens. If another local process ever took over your app's port, the inbound token it could capture opens nothing on Nebo.

When your app calls its own routes on Nebo's API, send `Token`:

```go
env := app.Env()
req, _ := http.NewRequest("GET", env.APIURL+"/api/v1/apps/"+env.ID+"/storage", nil)
req.Header.Set("Authorization", "Bearer "+env.Token)
```

## Handler Interfaces

| Capability | Interface | Manifest `provides` |
|-----------|-----------|---------------------|
| Tool | `ToolHandler` | `tool:<name>` |
| Channel | `ChannelHandler` | `channel:<name>` |
| Gateway | `GatewayHandler` | `gateway` |
| UI | `HandleFunc` / `Handle` | `ui` |
| Comm | `CommHandler` | `comm` |
| Schedule | `ScheduleHandler` | `schedule` |

## Schema Builder

Build JSON Schema for STRAP-pattern tool inputs:

```go
schema := nebo.NewSchema("list", "create", "delete").
    String("name", "Resource name", true).
    Number("limit", "Max results", false).
    Bool("verbose", "Show details", false).
    Enum("format", "Output format", false, "json", "text").
    Build()
```

## Documentation

See [Creating Nebo Apps](https://neboloop.com/developers) for the full guide.
