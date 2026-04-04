let ws = null;
let atlasImage = null;
let atlasOffset = { x: 0, y: 0, scale: 1 };
let atlasBounds = null;
let playerPos = null;
let waypoints = [];
let zLevel = 7;

const canvas = document.getElementById('map-canvas');
const ctx = canvas.getContext('2d');

function connectWS() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    ws = new WebSocket(`${proto}://${location.host}/ws`);
    ws.onmessage = (e) => {
        const data = JSON.parse(e.data);
        updateStatus(data);
    };
    ws.onclose = () => setTimeout(connectWS, 2000);
}

function updateStatus(data) {
    document.getElementById('state-badge').textContent = data.state || 'IDLE';
    if (data.health_pct !== undefined) {
        const hp = Math.round(data.health_pct);
        document.getElementById('health-bar').style.width = hp + '%';
        document.getElementById('health-val').textContent = hp + '%';
    }
    if (data.mana_pct !== undefined) {
        const mp = Math.round(data.mana_pct);
        document.getElementById('mana-bar').style.width = mp + '%';
        document.getElementById('mana-val').textContent = mp + '%';
    }
    if (data.position) {
        playerPos = data.position;
        document.getElementById('pos-val').textContent =
            `${data.position[0]}, ${data.position[1]}, ${data.position[2]}`;
    }
    if (data.waypoint_index !== undefined) {
        document.getElementById('wp-val').textContent = data.waypoint_index;
    }
    if (data.kills !== undefined) {
        document.getElementById('kills-val').textContent = data.kills;
    }
    drawMap();
}

async function startBot() {
    await fetch('/api/bot/start', { method: 'POST' });
    document.getElementById('btn-start').disabled = true;
    document.getElementById('btn-stop').disabled = false;
}

async function stopBot() {
    await fetch('/api/bot/stop', { method: 'POST' });
    document.getElementById('btn-start').disabled = false;
    document.getElementById('btn-stop').disabled = true;
}

async function loadAtlas() {
    zLevel = parseInt(document.getElementById('z-level').value);
    const boundsResp = await fetch(`/api/atlas/bounds/${zLevel}`);
    atlasBounds = await boundsResp.json();
    atlasImage = new Image();
    atlasImage.onload = drawMap;
    atlasImage.src = `/api/atlas/${zLevel}?t=${Date.now()}`;
}

function drawMap() {
    canvas.width = canvas.clientWidth;
    canvas.height = canvas.clientHeight;
    ctx.fillStyle = '#0f0f23';
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    if (!atlasImage || !atlasBounds) return;
    const scale = Math.min(canvas.width / atlasImage.width, canvas.height / atlasImage.height) * 0.9;
    const ox = (canvas.width - atlasImage.width * scale) / 2;
    const oy = (canvas.height - atlasImage.height * scale) / 2;
    atlasOffset = { x: ox, y: oy, scale: scale };
    ctx.drawImage(atlasImage, ox, oy, atlasImage.width * scale, atlasImage.height * scale);

    waypoints.forEach((wp, i) => {
        const px = ox + (wp.x - atlasBounds.min_x) * scale;
        const py = oy + (wp.y - atlasBounds.min_y) * scale;
        ctx.fillStyle = '#ff0';
        ctx.beginPath();
        ctx.arc(px, py, 5, 0, Math.PI * 2);
        ctx.fill();
        ctx.fillStyle = '#fff';
        ctx.font = '10px monospace';
        ctx.fillText(i + 1, px + 7, py + 3);
    });

    if (waypoints.length > 1) {
        ctx.strokeStyle = 'rgba(255,255,0,0.4)';
        ctx.lineWidth = 1;
        ctx.beginPath();
        waypoints.forEach((wp, i) => {
            const px = ox + (wp.x - atlasBounds.min_x) * scale;
            const py = oy + (wp.y - atlasBounds.min_y) * scale;
            if (i === 0) ctx.moveTo(px, py); else ctx.lineTo(px, py);
        });
        ctx.stroke();
    }

    if (playerPos && playerPos[2] === zLevel) {
        const px = ox + (playerPos[0] - atlasBounds.min_x) * scale;
        const py = oy + (playerPos[1] - atlasBounds.min_y) * scale;
        const blink = Math.sin(Date.now() / 200) > 0;
        ctx.fillStyle = blink ? '#0f0' : '#0a0';
        ctx.beginPath();
        ctx.arc(px, py, 6, 0, Math.PI * 2);
        ctx.fill();
        ctx.strokeStyle = '#fff';
        ctx.lineWidth = 1;
        ctx.stroke();
    }
}

canvas.addEventListener('click', (e) => {
    if (!atlasBounds || !atlasOffset.scale) return;
    const rect = canvas.getBoundingClientRect();
    const cx = e.clientX - rect.left;
    const cy = e.clientY - rect.top;
    const worldX = Math.round((cx - atlasOffset.x) / atlasOffset.scale + atlasBounds.min_x);
    const worldY = Math.round((cy - atlasOffset.y) / atlasOffset.scale + atlasBounds.min_y);
    waypoints.push({ x: worldX, y: worldY, z: zLevel, action: 'walk' });
    renderWaypointList();
    drawMap();
});

function clearWaypoints() {
    waypoints = [];
    renderWaypointList();
    drawMap();
}

function renderWaypointList() {
    const ol = document.getElementById('waypoint-list');
    ol.innerHTML = waypoints.map((wp) =>
        `<li>(${wp.x}, ${wp.y}, ${wp.z}) - ${wp.action}</li>`
    ).join('');
}

connectWS();
loadAtlas();
window.addEventListener('resize', drawMap);
setInterval(drawMap, 500);
