# TibiaBot

Vision-based Tibia cavebot — purely screen capture + computer vision, no memory
access (BattleEye safe).  Written with Claude Code.

## Features

- **Waypoint navigation** — walks a looping route defined by world coordinates
  (X, Y, Z); no minimap screenshot templates required
- **Concurrent modules** — healing, mana, combat and navigation all run at the
  same time via asyncio coroutines sharing a single 20 fps screen capture
- **Reachability detection** — detects when a monster is behind a wall and
  skips it instead of getting stuck
- **Smart loot** — only picks up items whose PNG template is in your whitelist;
  empty whitelist = collect nothing (no accidental backpack fill)
- **Waypoint recorder** — walk the route yourself, press INSERT at each spot,
  get a reusable JSON file

---

## Requirements

| Dependency | macOS | Windows | Linux |
|---|---|---|---|
| **Python 3.11+** | `brew install python` | [python.org](https://www.python.org/downloads/) | `sudo apt install python3` |
| **uv** (package manager) | see below | see below | see below |
| **Tesseract OCR** | `brew install tesseract` | [UB-Mannheim installer](https://github.com/UB-Mannheim/tesseract/wiki) | `sudo apt install tesseract-ocr` |
| **Git** | `brew install git` | [git-scm.com](https://git-scm.com/) | `sudo apt install git` |

---

## Installation

### macOS

```bash
# 1. Install Tesseract (coordinate OCR)
brew install tesseract

# 2. Install uv
curl -LsSf https://astral.sh/uv/install.sh | sh
source $HOME/.local/bin/env   # or restart terminal

# 3. Clone and install Python dependencies
git clone https://github.com/bartkoz/TibiaBot.git
cd TibiaBot
uv sync
```

### Windows (PowerShell)

```powershell
# 1. Install Tesseract – download and run the installer from:
#    https://github.com/UB-Mannheim/tesseract/wiki
#    During install, note the path (e.g. C:\Program Files\Tesseract-OCR)
#    Then add it to PATH:
$env:PATH += ";C:\Program Files\Tesseract-OCR"
# To make it permanent: System Properties → Environment Variables → Path

# 2. Install uv
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
# Restart PowerShell after this

# 3. Clone and install Python dependencies
git clone https://github.com/bartkoz/TibiaBot.git
cd TibiaBot
uv sync
```

### Linux (Ubuntu / Debian)

```bash
# 1. Install Tesseract
sudo apt update && sudo apt install -y tesseract-ocr git

# 2. Install uv
curl -LsSf https://astral.sh/uv/install.sh | sh
source $HOME/.local/bin/env   # or restart terminal

# 3. Clone and install Python dependencies
git clone https://github.com/bartkoz/TibiaBot.git
cd TibiaBot
uv sync
```

---

## Quick start

### Step 1 — Calibrate the UI regions

Open Tibia, log in, stand somewhere safe, then run:

```bash
# macOS / Linux
uv run python calibrate.py --show-coords

# Windows
uv run python calibrate.py --show-coords
```

This saves two images:
- `calibration_coords_roi.png` — what the OCR is looking at (should show digits)
- `calibration_coords_full.png` — full screenshot with the region highlighted

Edit `bot_config.yaml` → `coord_display` until the OCR reads the correct X, Y, Z.

```bash
uv run python calibrate.py --show-bars      # verify HP / mana bar detection
uv run python calibrate.py --show-viewport  # verify tile grid alignment
```

### Step 2 — Record your waypoints

Log in, go to the hunting spot, then run the recorder in a terminal:

```bash
uv run python record_waypoints.py waypoints/my_route.json
```

Switch back to Tibia.  Walk to each waypoint and press **INSERT** to stamp it.
Press **ESC** when done.

| Key | Action |
|---|---|
| `INSERT` | Record current position |
| `BACKSPACE` | Undo last waypoint |
| `Ctrl+S` | Save now (also auto-saved on every addition) |
| `Ctrl+L` | List all recorded waypoints |
| `ESC` | Save and quit |

To continue editing an existing route:

```bash
uv run python record_waypoints.py --load waypoints/my_route.json
```

### Step 3 — Add monster templates

For each monster type you want to hunt, the bot needs one screenshot so it
can tell when the character is actively attacking that monster.

1. Log in and find the monster.
2. Attack it — its name appears highlighted in the battle list on the right.
3. Take a screenshot of **just that battle-list entry** (the row showing the
   monster name while it is selected/being attacked).
4. Crop it tightly and save it as `images/{monster_name}_attacking.png`,
   e.g. `images/swamp_troll_attacking.png`.
5. Add the same name (without `_attacking.png`) to `combat.monsters` in
   `bot_config.yaml`.

```yaml
combat:
  monsters:
    - swamp_troll        # → bot looks for images/swamp_troll_attacking.png
    - dragon             # → bot looks for images/dragon_attacking.png
```

**Why only one image per monster?**
Detecting *whether an enemy is present* is done instantly via a single pixel
check on the battle list (no template needed, works for any creature).  The
`_attacking.png` template is only used to confirm the bot is already attacking
that specific monster so it doesn't try to start a second attack mid-fight.

> A repo ships with `images/swamp_troll_attacking.png` as an example.
> You will need to capture your own for any other monster.

### Step 4 — Configure the bot (bot_config.yaml)

Edit `bot_config.yaml`:

```yaml
navigation:
  waypoints_file: "waypoints/my_route.json"   # point to your recorded route

healing:
  hp_threshold: 70    # heal when HP drops below 70 %
  heal_key: "f1"      # hotkey you have bound to a healing spell / potion

loot:
  whitelist:
    - gold_coin        # add one entry per item; put gold_coin.png in loot/
```

For loot filtering, place a small PNG screenshot of each item icon into the
`loot/` directory.  The filename (without `.png`) must match the whitelist entry.
Use `["*"]` to take everything without filtering.

### Step 5 — Run the bot

```bash
# macOS / Linux
uv run python -m bot.main

# Windows
uv run python -m bot.main
```

Press **Ctrl+C** to stop.

---

## Configuration reference (`bot_config.yaml`)

```yaml
screen:
  width: 2560          # your screen resolution
  height: 1440
  capture_fps: 20      # screen grabs per second (shared by all modules)

viewport:
  center_x: 1185       # pixel X of your character's tile centre
  center_y: 610        # pixel Y of your character's tile centre
  tile_size: 64        # pixels per in-game tile at default zoom

coord_display:
  x: 1820              # top-left of the minimap coordinate text
  y: 372
  width: 180
  height: 14

healing:
  hp_threshold: 70     # heal when HP < this %
  heal_key: "f1"
  mana_threshold: 30   # use mana potion when mana < this %
  mana_key: null       # set to e.g. "f3", or null to disable

combat:
  monsters:
    # One entry per monster type you hunt.
    # Each name must have a matching images/{name}_attacking.png –
    # a screenshot of the battle-list row while that monster is selected.
    # See "Step 3 – Add monster templates" above for how to capture it.
    - swamp_troll            # → images/swamp_troll_attacking.png
  attack_key: "space"
  stuck_timeout: 3.0           # seconds without moving = unreachable
  unreachable_cooldown: 30.0   # seconds before retrying that area

navigation:
  waypoints_file: "waypoints/my_route.json"  # recommended
  # OR define inline:
  # waypoints:
  #   - [32372, 31949, 7]
  waypoint_tolerance: 2        # tiles – how close counts as arrived
  move_interval: 0.9           # seconds between movement clicks

loot:
  enabled: true
  delay_after_kill: 1.5        # wait this long before looting
  templates_dir: "loot"        # directory with item PNG templates
  whitelist:
    - gold_coin
    # - "*"                    # uncomment for take-all mode
```

---

## Project structure

```
TibiaBot/
├── bot/
│   ├── main.py              # entry point / asyncio engine
│   ├── screen.py            # background screen capture thread
│   ├── state.py             # shared game state
│   ├── vision.py            # template matching, bar reading, OCR
│   ├── config.py            # config dataclasses + YAML / JSON loader
│   └── modules/
│       ├── health.py        # HP monitoring + auto-heal
│       ├── mana.py          # mana monitoring + auto-recovery
│       ├── combat.py        # enemy detection, attack, reachability
│       ├── navigation.py    # coordinate-based waypoint following
│       └── loot.py          # whitelist-filtered loot collection
├── bot_config.yaml          # main configuration file
├── record_waypoints.py      # interactive waypoint recorder
├── calibrate.py             # UI region calibration helper
├── loot/                    # item PNG templates for loot whitelist
├── images/                  # UI element templates (health bar, battle list…)
└── waypoints/               # saved waypoint JSON files
```

---

## How it works

One background thread captures the screen at 20 fps.  All bot modules share
that single frame instead of issuing their own screen grabs, so there is no
redundant I/O.  Each module is an asyncio coroutine:

| Module | Check rate | What it does |
|---|---|---|
| Health | 50 ms | Reads HP bar, presses heal key when below threshold |
| Mana | 50 ms | Reads mana bar, presses mana key when below threshold |
| Combat | 50 ms | Pixel-checks battle list, attacks enemies, detects stuck |
| Navigation | 100 ms | OCR-reads minimap coords, clicks toward next waypoint |
| Loot | on-demand | Opens corpses, template-matches items, takes whitelist only |

---

## Branches

| Branch | Contents |
|---|---|
| `master` | Current v2 architecture |
| `legacy` | Original v1 code (archived for reference) |
