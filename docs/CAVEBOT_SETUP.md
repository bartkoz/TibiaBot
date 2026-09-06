# Cavebot Setup Guide (Windows)

Step-by-step instructions to get the cavebot running from scratch.

---

## Step 1: Install Dependencies

```bash
uv sync
```

---

## Step 2: Install OBS Studio

The bot reads the game screen through OBS Virtual Camera. OBS captures the Tibia window and exposes it as a webcam device that the bot reads via OpenCV.

Download and install from [obsproject.com](https://obsproject.com).

---

## Step 3: Add a Window Capture Source in OBS

1. Open OBS Studio
2. In the **Scenes** panel (bottom-left), click **+** to create a new scene, name it `Tibia`
3. In the **Sources** panel, click **+** and select **Window Capture**
4. Name it `tibia` and click OK
5. In the properties dialog, select the Tibia client window from the **Window** dropdown
6. **Uncheck "Capture Cursor"** — the cursor interferes with pixel reads
7. Click OK

> The Tibia client must be open before adding the source, otherwise it won't appear in the dropdown.

---

## Step 4: Configure OBS Canvas Resolution

The bot reads pixel regions at exact coordinates, so the output resolution must match the game.

1. Go to **Settings > Video**
2. Set **Base (Canvas) Resolution** to `1920x1080`
3. Set **Output (Scaled) Resolution** to `1920x1080` (same value — no downscaling)
4. Set **FPS** to `20` (the bot doesn't need more; saves CPU)
5. Click **Apply**

---

## Step 5: Fit Source to Canvas

The Tibia game must fill the entire OBS canvas with no black borders:

1. Right-click the source in the OBS preview area
2. Select **Transform > Fit to Screen** (or press Ctrl+F)
3. Verify the game image fills the entire preview — no black bars

---

## Step 6: Remove Overlays and Filters

The bot does pixel-perfect reads of HP bars, minimap, and battle list. Any visual alteration breaks detection.

- No text overlays, webcam sources, or image sources in the scene
- No color correction, sharpening, or other filters on the capture source
- No crop/pad filters unless intentionally isolating the game area

---

## Step 7: Start the Virtual Camera

1. In the OBS main window, click **Start Virtual Camera** (bottom-right Controls panel)
2. The button changes to "Stop Virtual Camera" when active
3. Keep OBS running — the bot reads from this virtual camera continuously

> The virtual camera must be started **before** launching the bot.

---

## Step 8: Find the Camera Index

The bot needs to know which device number is the OBS Virtual Camera.

```bash
uv run python -c "
import cv2
for i in range(10):
    cap = cv2.VideoCapture(i)
    if cap.isOpened():
        w = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH))
        h = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
        print(f'Index {i}: {w}x{h}')
        cap.release()
    else:
        break
"
```

Look for the entry showing `1920x1080`. That index number goes into your config as `camera_index`. If you have no webcam, it's usually `0`.

---

## Step 9: Verify the Capture

Save a test frame to confirm everything works:

```bash
uv run python -c "
import cv2
cap = cv2.VideoCapture(0)
ret, frame = cap.read()
if ret:
    cv2.imwrite('test_capture.png', frame)
    print(f'Saved test_capture.png ({frame.shape[1]}x{frame.shape[0]})')
else:
    print('ERROR: could not read frame')
cap.release()
"
```

Replace `0` with your camera index from Step 8 if different.

Open `test_capture.png` and verify:
- The Tibia client is fully visible
- No black borders or scaling artifacts
- Resolution is 1920×1080

**Keep this image open** — you'll use it to measure pixel coordinates.

---

## Step 10: Create Your Config File

```bash
cp configs/default.yaml configs/mychar.yaml
```

---

## Step 11: Calibrate Screen Regions

Open `test_capture.png` in Paint or any image editor. Measure the pixel coordinates of these UI elements:

| Region | What to find | Format |
|--------|-------------|--------|
| `minimap_region` | The square minimap (top-right corner) | `[x, y, width, height]` |
| `health_bar_region` | The red HP bar | `[x, y, width, height]` |
| `mana_bar_region` | The blue mana bar | `[x, y, width, height]` |
| `battle_list_region` | The creature/battle list panel | `[x, y, width, height]` |

Where `x, y` is the **top-left corner** of the element, and `width, height` is its size in pixels.

Update your config:

```yaml
capture:
  camera_index: 0                            # from Step 8
  minimap_region: [1650, 20, 110, 110]       # adjust to your layout
  health_bar_region: [10, 50, 200, 15]       # adjust to your layout
  mana_bar_region: [10, 70, 200, 15]         # adjust to your layout
  battle_list_region: [1650, 200, 180, 300]  # adjust to your layout
```

---

## Step 12: Copy Minimap Data

The bot uses Tibia's minimap files for position detection and pathfinding.

1. Find your Tibia minimap directory:
   ```
   %APPDATA%\Tibia\packages\Tibia\minimap\
   ```

2. Copy all files into the bot's data directory:
   ```bash
   xcopy "%APPDATA%\Tibia\packages\Tibia\minimap\*" data\minimap\ /Y
   ```

The bot expects two file types per area:
- `Minimap_Color_{x}_{y}_{z}.png` — visual tiles (position detection)
- `Minimap_WaypointCost_{x}_{y}_{z}.png` — walkability costs (pathfinding)

> You **must** have walked through the areas you want the bot to navigate in-game. Unexplored areas have no minimap data and will be treated as blocked.

---

## Step 13: Configure Healing

Map these to your in-game hotkeys:

```yaml
healing:
  health_pot_key: "F1"      # key bound to health potion in Tibia
  health_threshold: 60       # heal when HP drops below 60%
  health_cooldown: 1.0       # seconds between heals
  mana_pot_key: "F2"         # key bound to mana potion
  mana_threshold: 40         # drink mana when below 40%
  mana_cooldown: 1.0
```

---

## Step 14: Configure Combat

```yaml
combat:
  attack_key: "F3"            # key to attack target
  spell_keys: ["F4", "F5"]    # offensive spell hotkeys
  spell_cooldowns: [2.0, 6.0] # cooldown per spell in seconds
```

---

## Step 15: Configure Movement

```yaml
movement:
  step_delay: 0.2   # seconds between movement steps (lower = faster)
```

---

## Step 16: Set the Minimap Path

```yaml
minimap:
  data_path: "./data/minimap"
  start_z: 7                  # 7 = surface level
```

---

## Step 17: Add Waypoints

Waypoints define the cavebot's walking loop.

### Method A: Web UI (recommended)

1. Start the bot server:
   ```bash
   uv run python -m cavebot --config configs/mychar.yaml
   ```
2. Open `http://127.0.0.1:8080` in your browser
3. Select the **Z level** (7 = surface, 8+ = underground)
4. **Click on the map** to place waypoints — yellow numbered dots appear
5. Waypoints are followed in order: 1 → 2 → 3 → ... → back to 1
6. Copy the coordinates from the waypoint list into your YAML config:
   ```yaml
   waypoints:
     list:
       - {x: 32369, y: 32241, z: 7, action: "walk"}
       - {x: 32380, y: 32241, z: 7, action: "walk"}
       - {x: 32380, y: 32260, z: 7, action: "walk"}
       - {x: 32369, y: 32260, z: 7, action: "walk"}
     loop: true
   ```
7. Use the **Clear** button to start over if needed

### Method B: Manual YAML

If you know the world coordinates (from Tibia wikis or in-game):

```yaml
waypoints:
  list:
    - {x: 32369, y: 32241, z: 7, action: "walk"}
    - {x: 32380, y: 32241, z: 7, action: "walk"}
    - {x: 32380, y: 32260, z: 7, action: "walk"}
  loop: true
```

### Waypoint field reference

| Field  | Type   | Description |
|--------|--------|-------------|
| `x`    | int    | World X coordinate (typically 31000-33000) |
| `y`    | int    | World Y coordinate (typically 31000-33000) |
| `z`    | int    | Floor: 0-6 = above ground, 7 = surface, 8-15 = underground |
| `action` | string | Action at waypoint. Currently only `"walk"` is supported |

Set `loop: true` to repeat forever, `false` to stop after one cycle.

### Waypoint tips

- Place waypoints at **turns, corners, and doorways** — the bot pathfinds between consecutive points
- Don't space them too far apart — A* pathfinding works best over moderate distances
- Keep waypoints on the **same Z level** — the pathfinder doesn't cross floors
- Ensure you have **both** Color and WaypointCost tiles for the area

---

## Step 18: Run the Bot

```bash
# Normal start
uv run python -m cavebot --config configs/mychar.yaml

# Custom port
uv run python -m cavebot --config configs/mychar.yaml --port 9090

# Debug logging
uv run python -m cavebot --config configs/mychar.yaml --log-level DEBUG
```

Open `http://127.0.0.1:8080` and click **Start**. The bot will:
1. Load the minimap atlas into memory
2. Start reading frames from the OBS virtual camera
3. Detect your position via minimap template matching
4. Pathfind to the first waypoint
5. Walk the route, healing and fighting as configured
6. Loop back to waypoint 1 when done (if `loop: true`)

### State machine

```
IDLE ──Start──> WALKING ──enemy detected──> COMBAT
                  ^   |                       |
                  |   └──supplies low──> REFILL
                  |                        |
                  └──no enemy──── LOOTING <┘
```

- **WALKING** — following path to next waypoint
- **COMBAT** — enemy in battle list, casting spells
- **LOOTING** — post-combat, transitions back to walking
- **REFILL** — navigating to depot waypoint for supplies
- **IDLE** — bot stopped

---

## Supplies (optional)

To have the bot return to depot when potions run low:

```yaml
supplies:
  max_potions: 200        # starting potion count
  refill_threshold: 20    # go to depot when this many remain
  depot_waypoint_index: 0 # which waypoint index is the depot
```

---

## Complete Example Config

```yaml
capture:
  camera_index: 0
  minimap_region: [1650, 20, 110, 110]
  health_bar_region: [10, 50, 200, 15]
  mana_bar_region: [10, 70, 200, 15]
  battle_list_region: [1650, 200, 180, 300]

minimap:
  data_path: "./data/minimap"
  start_z: 7

waypoints:
  list:
    - {x: 32369, y: 32241, z: 7, action: "walk"}
    - {x: 32380, y: 32241, z: 7, action: "walk"}
    - {x: 32380, y: 32260, z: 7, action: "walk"}
    - {x: 32369, y: 32260, z: 7, action: "walk"}
  loop: true

healing:
  health_pot_key: "F1"
  health_threshold: 60
  health_cooldown: 1.0
  mana_pot_key: "F2"
  mana_threshold: 40
  mana_cooldown: 1.0

combat:
  attack_key: "F3"
  spell_keys: ["F4", "F5"]
  spell_cooldowns: [2.0, 6.0]

movement:
  step_delay: 0.2

supplies:
  max_potions: 200
  refill_threshold: 20
  depot_waypoint_index: 0

web:
  host: "127.0.0.1"
  port: 8080
```

---

## Troubleshooting

| Problem | Cause | Fix |
|---------|-------|-----|
| `ERROR: could not read frame` | Virtual Camera not started or wrong index | Click "Start Virtual Camera" in OBS, re-run camera index script |
| OBS preview is black | OBS not running as admin, or Tibia not open | Try running OBS as administrator; ensure Tibia is open before adding source |
| `cv2.error` in `matchTemplate` | `minimap_region` too large (>256px) | Re-measure from `test_capture.png` — minimap should be ~110×110px |
| "No path found to waypoint N" | Missing minimap data | Walk through the area in-game. Check both Color and WaypointCost PNGs exist |
| Bot doesn't detect position | Wrong `minimap_region` coordinates | Re-measure from `test_capture.png` |
| HP/mana always reads 0% or 100% | Wrong bar region coordinates | Re-measure `health_bar_region` / `mana_bar_region` from `test_capture.png` |
| Bot doesn't attack | Wrong battle list region or key | Verify `battle_list_region` from screenshot, check `attack_key` matches Tibia hotkey |
| Atlas blank in web UI | Wrong `data_path` or no tiles for Z level | Check path exists and contains Minimap_Color PNGs, try different Z level |
