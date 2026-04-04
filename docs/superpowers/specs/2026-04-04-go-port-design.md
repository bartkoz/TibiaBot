# Go Port Design Spec

## Overview

1:1 port of the TibiaBot Python cavebot to Go. Same features, same config format, same web UI. Goal: single-binary distribution for Windows and macOS.

## Project Structure

```
go-cavebot/
├── cmd/cavebot/main.go
├── internal/
│   ├── core/
│   │   ├── bot.go            # BotState, BotStateMachine, CaveBot orchestrator
│   │   ├── config.go         # Config structs + YAML loader
│   │   └── input.go          # Keyboard input via robotgo
│   ├── vision/
│   │   ├── capture.go        # GoCV VideoCapture + region crop
│   │   ├── health.go         # Bar % reading (pixel counting)
│   │   ├── detection.go      # Battle list / loot window detection
│   │   └── minimap.go        # Template matching position locator
│   ├── navigation/
│   │   ├── atlas.go          # Chunk loading, cost grid indexing
│   │   ├── pathfinder.go     # A* with octile heuristic
│   │   └── walker.go         # Direction enum, path to keypresses
│   ├── combat/
│   │   ├── fighter.go        # Spell rotation + cooldowns
│   │   └── targeting.go      # Battle list detection wrapper
│   ├── survival/
│   │   ├── healer.go         # HP/mana threshold checks
│   │   └── supplies.go       # Potion tracking + refill logic
│   └── web/
│       ├── server.go         # HTTP routes + WebSocket handler
│       ├── ws.go             # Connection manager + broadcast
│       └── static/           # index.html, app.js, style.css (unchanged)
├── configs/
│   ├── default.yaml
│   └── test.yaml
├── data/minimap/             # PNG atlas tiles (shared with Python)
├── go.mod
└── go.sum
```

## Dependencies

| Purpose | Package |
|---|---|
| Computer vision | `gocv.io/x/gocv` |
| Keyboard input | `github.com/go-vgo/robotgo` |
| WebSocket | `github.com/gorilla/websocket` |
| YAML config | `gopkg.in/yaml.v3` |
| CLI flags | `flag` (stdlib) |
| HTTP server | `net/http` (stdlib) |
| Testing | `testing` (stdlib) |
| Image decode | `image/png` (stdlib) |

## Concurrency Model

- **Main goroutine**: HTTP server via `net/http.ListenAndServe`
- **Bot goroutine**: Started on `POST /api/bot/start`, runs 10fps main loop
- **Frame capture**: Sub-goroutine, writes `gocv.Mat` behind `sync.RWMutex`
- **GameState**: Struct with `sync.RWMutex`, same fields as Python `BotStateMachine`
- **Status broadcast**: Bot pushes to a channel, WebSocket handler reads and broadcasts
- **Shutdown**: `context.Context` with cancel, bot checks `ctx.Done()`

## Module Translation

### core/bot.go
- `BotState` as `const iota` (Idle, Walking, Combat, Looting, Healing, Refill)
- `BotStateMachine` struct: position, health%, mana%, kills, waypoint index, event log, RWMutex
- `CaveBot` struct: owns all subsystems, runs main loop goroutine at 10fps
- State transitions: Walking -> Combat -> Looting -> Walking, Walking -> Refill

### core/config.go
- Nested Go structs with `yaml` struct tags
- `LoadConfig(path string) (*Config, error)` reads YAML
- Same field names/types as Python dataclasses
- Config YAML schema unchanged — same files work for both versions

### core/input.go
- `PressKey(key string)` wraps `robotgo.KeyTap`
- `PressKeysSimultaneous(keys []string)` wraps `robotgo.KeyToggle` pairs
- Key mapping: arrow keys, F1-F12

### vision/capture.go
- `FrameCapture` struct wrapping `gocv.VideoCapture`
- `Open(cameraIndex int) error`
- `Read() gocv.Mat` (returns current frame)
- `CropRegion(frame gocv.Mat, region [4]int) gocv.Mat` using `Mat.Region()`
- Background goroutine reads frames, stores behind RWMutex

### vision/health.go
- `ReadBarPercentage(bar gocv.Mat, channel int, threshold uint8) float64`
- Reads middle row, counts pixels above threshold in given channel
- Returns 0.0-100.0

### vision/detection.go
- `IsBattleListActive(region gocv.Mat, brightnessThreshold uint8, minBrightRatio float64) bool`
- `IsLootWindowOpen(region gocv.Mat, ...) bool`

### vision/minimap.go
- `MinimapLocator` struct with cached last chunk
- `Locate(minimap gocv.Mat) (x, y int, confidence float64)`
- Uses `gocv.MatchTemplate` with `gocv.TmCcoeffNormed`
- Searches neighbors of last chunk first, falls back to full z-level

### navigation/atlas.go
- `Atlas` struct: chunks map `[chunkKey]gocv.Mat` for color, `[chunkKey]*image.Gray` for cost
- `Load(dataPath string, startZ int) error`
- `GetCostAt(x, y, z int) uint8` — returns 255 if blocked
- Chunk size: 256x256

### navigation/pathfinder.go
- `FindPath(atlas *Atlas, start, goal [2]int, maxIter int) [][2]int`
- A* with octile heuristic, 8-directional movement
- Priority queue via `container/heap`
- Cost: `moveCost * (tileCost / 150.0)`, blocked at 255

### navigation/walker.go
- `Direction` type with 8 constants, each mapping to arrow key(s)
- `PathToDirections(path [][2]int) []Direction`
- `WalkPath(path [][2]int, stepDelay time.Duration)`

### combat/fighter.go
- `SpellRotation` struct: map of key -> last used `time.Time`
- `NextReadySpell() (string, bool)`
- `MarkUsed(key string)`
- `CastNext()` presses key if available
- `Attack(key string)` single key tap

### combat/targeting.go
- `TargetingSystem` struct wrapping detection call

### survival/healer.go
- `Healer` struct: thresholds, cooldowns per potion type
- `Check(healthPct, manaPct float64) string` returns "health", "mana", or ""

### survival/supplies.go
- `SupplyTracker` struct: remaining count, max, threshold
- `NeedsRefill() bool`
- `UsePotion()`
- `Refill()`

### web/server.go
- `net/http` handlers for all routes
- `GET /` serves embedded static files (or filesystem)
- `GET /api/config`, `POST /api/config`
- `POST /api/bot/start`, `POST /api/bot/stop`, `GET /api/bot/status`
- `GET /api/atlas/{z}` renders composite atlas PNG
- `GET /api/atlas/bounds/{z}` returns bounds JSON
- `WebSocket /ws` upgrades via gorilla/websocket

### web/ws.go
- `ConnectionManager` struct: slice of `*websocket.Conn` behind mutex
- `Add(conn)`, `Remove(conn)`, `Broadcast(data []byte)`

## Testing Strategy

- One `_test.go` per source file, same package (white-box)
- Table-driven tests
- Synthetic images via `gocv.NewMatWithSize` + pixel writes
- Test files map to Python:
  - `bot_test.go`, `config_test.go`, `input_test.go`
  - `capture_test.go`, `health_test.go`, `detection_test.go`, `minimap_test.go`
  - `atlas_test.go`, `pathfinder_test.go`, `walker_test.go`
  - `fighter_test.go`, `targeting_test.go`
  - `healer_test.go`, `supplies_test.go`
  - `server_test.go`

## Cross-Platform

- `robotgo` handles Windows + macOS + Linux input
- `gocv` wraps OpenCV which is cross-platform
- Build with `CGO_ENABLED=1` (required for gocv + robotgo)
- Distribution: prebuilt binaries per OS via `go build`

## Config Format

Unchanged from Python. Same YAML schema, same files. Example:

```yaml
capture:
  camera_index: 0
  minimap_region: [1633, 44, 106, 109]
  health_region: [...]
  mana_region: [...]
  battle_region: [...]
minimap:
  data_path: "data/minimap"
  start_z: 7
waypoints:
  loop: true
  points: [[32100, 32200], ...]
healing:
  health_pot_key: "F1"
  mana_pot_key: "F2"
  health_threshold: 60
  mana_threshold: 40
combat:
  attack_key: "F3"
  spell_keys: ["F4", "F5"]
  spell_cooldown: 2.0
movement:
  step_delay: 0.2
supplies:
  max_potions: 100
  refill_threshold: 20
  depot_waypoint_index: 0
web:
  host: "127.0.0.1"
  port: 8080
```
