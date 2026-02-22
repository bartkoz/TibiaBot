# TibiaBot

Vision-based Tibia cavebot — purely screen capture + computer vision, no memory
access (BattleEye safe).  Written with Claude Code.

## Features

- **Minimap visual-odometry navigation** — records minimap snapshots at each
  patrol point; navigates by template-matching the live minimap feed.  No
  coordinate display or OCR required
- **OCR coordinate navigation** — alternative mode that reads the on-screen
  X, Y, Z display and clicks toward exact world coordinates
- **Concurrent modules** — healing, mana, combat, navigation and loot all run
  simultaneously via asyncio coroutines sharing a single 20 fps screen capture
- **Smart loot** — only picks up items whose PNG template is in your whitelist;
  empty whitelist = collect nothing
- **Loot recorder** — hover over item icons in a container, press INSERT, get
  PNGs + `bot_config.yaml` whitelist entry automatically
- **Waypoint recorders** — two tools: minimap-snapshot recorder (no OCR needed)
  and classic coordinate recorder (needs OCR display)

---

## Requirements

| Dependency | macOS | Windows | Linux |
|---|---|---|---|
| **Python 3.11+** | `brew install python` | [python.org](https://www.python.org/downloads/) | `sudo apt install python3` |
| **uv** (package manager) | see below | see below | see below |
| **Tesseract OCR** *(optional)* | `brew install tesseract` | [UB-Mannheim installer](https://github.com/UB-Mannheim/tesseract/wiki) | `sudo apt install tesseract-ocr` |

> Tesseract is only required if you use **OCR coordinate navigation**
> (`record_waypoints.py` + `navigation:` in config).  The minimap visual-odometry
> mode (`record_minimap_waypoints.py` + `minimap:`) works without it.

---

## Installation

### macOS / Linux

```bash
# 1. Install uv
curl -LsSf https://astral.sh/uv/install.sh | sh
source $HOME/.local/bin/env   # or restart terminal

# 2. Clone and install Python dependencies
git clone https://github.com/bartkoz/TibiaBot.git
cd TibiaBot
uv sync
```

### Windows (PowerShell)

```powershell
# 1. Install uv
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
# Restart PowerShell after this

# 2. Clone and install Python dependencies
git clone https://github.com/bartkoz/TibiaBot.git
cd TibiaBot
uv sync
```

---

## Quick start

### Step 1 — Calibrate the viewport and bars

```bash
uv run python calibrate.py --show-bars      # verify HP / mana bar detection
uv run python calibrate.py --show-viewport  # verify tile grid alignment
```

Edit `bot_config.yaml` → `viewport` until the tile grid lines up with in-game tiles.

### Step 2 — Calibrate the minimap region

```bash
uv run python calibrate.py --show-minimap
```

Open `calibration_minimap_zoom.png`.  The **green box** must cover exactly the
minimap image.  Adjust `minimap.x / y / width / height` in `bot_config.yaml`
until it does.

The **red cross** shows the player-dot position (should land on the white `+`
in the minimap).  The **cyan box** shows the template crop area.

### Step 3 — Record your patrol route

```bash
uv run python record_minimap_waypoints.py
```

Switch to Tibia.  Walk to each patrol point and press **INSERT**.  A
`minimap_wp_preview.png` is saved after each stamp so you can confirm the
capture.  Press **ESC** when done.

| Key | Action |
|---|---|
| `INSERT` | Save minimap snapshot as next waypoint |
| `BACKSPACE` | Remove last recorded waypoint |
| `Ctrl+S` | Save now (also auto-saved on every change) |
| `Ctrl+L` | List all recorded waypoints |
| `ESC` | Save and quit |

### Step 4 — Record loot templates

```bash
uv run python record_loot.py
```

Open a container in Tibia.  Hover over each item icon and press **INSERT**,
type a name, press Enter.  The PNG is saved to `loot/` and the item is added
to `bot_config.yaml` automatically.

### Step 5 — Configure and run

Edit `bot_config.yaml`:

```yaml
minimap:
  enabled: true
  waypoints_file: "waypoints/minimap_route.json"

healing:
  hp_threshold: 70    # heal when HP drops below 70 %
  heal_key: "f1"

loot:
  whitelist:
    - gold_coin
```

Then run:

```bash
uv run python -m bot.main
```

Press **Ctrl+C** to stop.

---

## Navigation modes

### Minimap visual odometry (recommended)

Does **not** require the X, Y, Z coordinate display in the Tibia client.

1. Calibrate: `calibrate.py --show-minimap`
2. Record:    `record_minimap_waypoints.py`
3. Enable in config:

```yaml
minimap:
  enabled: true
  waypoints_file: "waypoints/minimap_route.json"
```

The bot template-matches each waypoint's minimap snapshot against the live
feed to determine direction and arrival.  All waypoints must be within
`(minimap_width − template_size) / 2` pixels of each other on the minimap
(≈ 33 tiles with the defaults).

### OCR coordinate navigation (alternative)

Requires the X, Y, Z text to be visible somewhere on screen.

1. Calibrate: `calibrate.py --show-coords`
2. Record:    `record_waypoints.py`
3. Enable in config:

```yaml
navigation:
  enabled: true
  waypoints_file: "waypoints/my_route.json"
```

When `minimap.enabled: true` the minimap mode takes priority; OCR navigation
is used only when `minimap.enabled: false` and `navigation.enabled: true`.

---

## Configuration reference (`bot_config.yaml`)

```yaml
screen:
  width: 2560          # your screen resolution
  height: 1440
  capture_fps: 20

viewport:
  center_x: 1185       # pixel X of your character's tile centre
  center_y: 610        # pixel Y of your character's tile centre
  tile_size: 64        # pixels per in-game tile at default zoom

healing:
  hp_threshold: 70     # heal when HP < this %
  heal_key: "f1"
  mana_threshold: 30
  mana_key: null       # e.g. "f3", or null to disable

combat:
  attack_key: "space"
  stuck_timeout: 3.0
  unreachable_cooldown: 30.0
  attack_indicator_offset: [-10, -10]

# ── Minimap visual-odometry navigation (recommended) ──────────────────────
minimap:
  enabled: true
  x: 1633              # minimap region on screen (calibrate.py --show-minimap)
  y: 44
  width: 106
  height: 109
  template_size: 40    # centre-crop size saved per waypoint
  arrival_px: 8        # pixels from centre = "arrived"
  move_interval: 0.9
  stuck_timeout: 5.0
  waypoints_file: "waypoints/minimap_route.json"

# ── OCR coordinate navigation (alternative, requires coord display) ────────
navigation:
  enabled: false
  waypoints_file: "waypoints/my_route.json"
  waypoint_tolerance: 2
  move_interval: 0.9

loot:
  enabled: true
  delay_after_kill: 1.5
  templates_dir: "loot"
  whitelist:
    - gold_coin
    # - "*"            # take-all mode
```

---

## Project structure

```
TibiaBot/
├── bot/
│   ├── main.py                   # entry point / asyncio engine
│   ├── screen.py                 # background screen capture thread
│   ├── state.py                  # shared game state
│   ├── vision.py                 # template matching, bar reading, OCR
│   ├── config.py                 # config dataclasses + YAML loader
│   └── modules/
│       ├── health.py             # HP monitoring + auto-heal
│       ├── mana.py               # mana monitoring + auto-recovery
│       ├── combat.py             # enemy detection, attack, reachability
│       ├── navigation.py         # OCR coordinate waypoint following
│       ├── minimap_navigation.py # minimap visual-odometry navigation
│       └── loot.py               # whitelist-filtered loot collection
├── bot_config.yaml               # main configuration file
├── calibrate.py                  # UI region calibration helper
├── record_minimap_waypoints.py   # minimap waypoint recorder (no OCR needed)
├── record_waypoints.py           # OCR coordinate waypoint recorder
├── record_loot.py                # loot template recorder
├── loot/                         # item PNG templates for loot whitelist
├── images/                       # UI element templates (health bar, battle list…)
└── waypoints/                    # saved waypoint JSON / PNG template files
```

---

## How it works

One background thread captures the screen at 20 fps.  All bot modules share
that single frame so there is no redundant I/O.  Each module is an asyncio
coroutine:

| Module | Rate | What it does |
|---|---|---|
| Health | 50 ms | Reads HP bar, presses heal key when below threshold |
| Mana | 50 ms | Reads mana bar, presses mana key when below threshold |
| Combat | 50 ms | Pixel-checks battle list, attacks enemies, detects stuck |
| MinimapNavigation | 100 ms | Template-matches minimap, clicks toward next waypoint |
| Navigation | 100 ms | OCR-reads minimap coords, clicks toward next waypoint |
| Loot | on-demand | Opens corpses, template-matches items, takes whitelist only |

---

## Branches

| Branch | Contents |
|---|---|
| `master` | Current v2 architecture |
| `legacy` | Original v1 code (archived for reference) |
