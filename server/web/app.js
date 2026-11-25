'use strict';
if (location.search) history.replaceState(null, '', location.pathname);

const api = async (path, opts = {}) => {
  const res = await fetch(path, { ...opts, headers: { 'X-Radspur': '1', 'Content-Type': 'application/json', ...(opts.headers || {}) } });
  if (res.status === 401) { location.href = '/login'; throw new Error('unauthorized'); }
  if (!res.ok) throw new Error(await res.text());
  return res.status === 204 ? null : res.json();
};

const fmtTime = ts => new Date(ts).toLocaleString('de-AT');
const ago = ts => {
  const s = Math.round((Date.now() - ts) / 1000);
  if (s < 60) return `vor ${s} s`;
  if (s < 3600) return `vor ${Math.round(s / 60)} min`;
  if (s < 86400) return `vor ${Math.round(s / 3600)} h`;
  return `vor ${Math.round(s / 86400)} Tagen`;
};
const fmtDur = s => `${Math.floor(s / 3600)}:${String(Math.floor(s / 60) % 60).padStart(2, '0')} h`;
const sportLabel = t => t.type === 'run' ? 'Laufen' : 'Rad';
const isRun = t => t.type === 'run';
const pace = (m, s) => { if (!m || !s) return '–'; const spk = Math.round(s / (m / 1000)); return `${Math.floor(spk / 60)}:${String(spk % 60).padStart(2, '0')} /km`; };
const esc = s => String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

const tiles = () => L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
  maxZoom: 19, attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>-Mitwirkende',
  referrerPolicy: 'strict-origin-when-cross-origin' // OSM-Kachelserver verlangen einen Referer
});

// ---------- tabs ----------
let tourMap;
document.querySelectorAll('nav button').forEach(b => b.addEventListener('click', () => {
  document.querySelectorAll('nav button').forEach(x => x.classList.toggle('active', x === b));
  document.querySelectorAll('.tab').forEach(t => t.hidden = t.id !== 'tab-' + b.dataset.tab);
  const tab = b.dataset.tab;
  if (tab === 'tours') { initTourMap(); loadTours(); }
  else if (tab === 'stats') loadStats();
  else if (tab === 'account') loadAccount();
  else if (tab === 'users') loadUsers();
  else phoneMap.invalidateSize();
}));

// ---------- phone ----------
const phoneMap = L.map('map-phone').setView([47.5, 13.5], 7);
tiles().addTo(phoneMap);
let marker, circle, trail, centered = false;

async function refreshPhone() {
  const st = await api('/api/web/status');
  const info = document.getElementById('phone-info');
  const l = st.last;
  if (l) {
    const pos = [l.lat, l.lon];
    if (!marker) { marker = L.marker(pos).addTo(phoneMap); circle = L.circle(pos, { radius: l.acc || 0 }).addTo(phoneMap); }
    marker.setLatLng(pos); circle.setLatLng(pos); circle.setRadius(l.acc || 0);
    if (!centered) { phoneMap.setView(pos, 16); centered = true; }
    info.innerHTML = `<dl>
      <dt>Zuletzt</dt><dd>${fmtTime(l.ts)} (${ago(l.ts)})</dd>
      <dt>Genauigkeit</dt><dd>${l.acc != null ? Math.round(l.acc) + ' m' : '–'}</dd>
      <dt>Akku</dt><dd>${l.battery != null ? l.battery + ' %' + (l.charging ? ' (lädt)' : '') : '–'}</dd>
      <dt>Quelle</dt><dd>${esc(l.source || '–')}</dd>
      <dt>Koordinaten</dt><dd><a href="https://www.openstreetmap.org/?mlat=${l.lat}&mlon=${l.lon}#map=17/${l.lat}/${l.lon}" target="_blank" rel="noopener">${l.lat.toFixed(5)}, ${l.lon.toFixed(5)}</a></dd>
    </dl>`;
  }
  const names = { ring: 'Klingeln', stop_ring: 'Klingeln stoppen', locate: 'Position anfordern' };
  document.getElementById('pending').textContent = st.pending.length
    ? 'Wartet auf Handy: ' + st.pending.map(c => names[c.type] + (c.delivered ? ' (zugestellt)' : '')).join(', ')
    : '';
}

async function refreshHistory() {
  const h = +document.getElementById('history').value;
  if (trail) { trail.remove(); trail = null; }
  if (!h) return;
  const pts = await api('/api/web/locations?since=' + (Date.now() - h * 3600e3));
  trail = L.polyline(pts.map(p => [p.lat, p.lon]), { color: '#1f6feb', weight: 3, opacity: .6 }).addTo(phoneMap);
}

const sendCmd = type => api('/api/web/commands', { method: 'POST', body: JSON.stringify({ type }) }).then(refreshPhone).catch(e => alert(e.message));
document.getElementById('btn-locate').onclick = () => sendCmd('locate');
document.getElementById('btn-ring').onclick = () => confirm('Handy jetzt mit voller Lautstärke klingeln lassen?') && sendCmd('ring');
document.getElementById('btn-stop').onclick = () => sendCmd('stop_ring');
document.getElementById('history').onchange = refreshHistory;

refreshPhone(); refreshHistory();
setInterval(refreshPhone, 15000);

// ---------- tours ----------
let tourLine;
function initTourMap() {
  if (tourMap) { tourMap.invalidateSize(); return; }
  tourMap = L.map('map-tour').setView([47.5, 13.5], 7);
  tiles().addTo(tourMap);
}

async function loadTours() {
  const tours = await api('/api/web/tours');
  const list = document.getElementById('tour-list');
  if (!tours.length) { list.innerHTML = '<p class="muted">Noch keine Touren hochgeladen.</p>'; return; }
  list.innerHTML = tours.map(t => `<div class="tour" data-id="${t.id}">
      <div><span class="badge ${isRun(t) ? 'run' : 'bike'}">${sportLabel(t)}</span> ${esc(t.name)}</div>
      <small>${fmtTime(t.startTs)} · ${(t.distanceM / 1000).toFixed(1)} km · ${fmtDur(t.movingS)}${isRun(t) ? ' · ' + pace(t.distanceM, t.movingS) : ''}</small>
    </div>`).join('');
  list.querySelectorAll('.tour').forEach(el => el.onclick = () => showTour(el.dataset.id));
}

async function showTour(id) {
  document.querySelectorAll('.tour').forEach(el => el.classList.toggle('sel', el.dataset.id === id));
  const t = await api('/api/web/tours/' + id);
  const avg = t.movingS ? t.distanceM / t.movingS * 3.6 : 0;
  document.getElementById('tour-detail').innerHTML = `<div class="stats">
      <strong>${esc(t.name)}</strong>
      <span><b>${(t.distanceM / 1000).toFixed(2)}</b> km</span>
      <span><b>${fmtDur(t.movingS)}</b> in Bewegung</span>
      <span class="badge ${isRun(t) ? 'run' : 'bike'}">${sportLabel(t)}</span>
      ${isRun(t) ? `<span>Ø <b>${pace(t.distanceM, t.movingS)}</b></span>` : `<span>Ø <b>${avg.toFixed(1)}</b> km/h</span>`}
      <span>max <b>${(t.maxSpeed * 3.6).toFixed(1)}</b> km/h</span>
      <span>↑ <b>${Math.round(t.elevGain)}</b> m</span>
      <a href="/tours/${t.id}/gpx">GPX</a>
      <button id="del-tour" class="link">Löschen</button>
    </div>`;
  document.getElementById('del-tour').onclick = async () => {
    if (!confirm('Tour wirklich löschen?')) return;
    await api('/api/web/tours/' + t.id, { method: 'DELETE' });
    if (tourLine) tourLine.remove();
    document.getElementById('tour-detail').textContent = 'Tour auswählen.';
    loadTours();
  };
  if (tourLine) tourLine.remove();
  tourLine = L.polyline(t.points.map(p => [p.lat, p.lon]), { color: '#c62828', weight: 4 }).addTo(tourMap);
  tourMap.fitBounds(tourLine.getBounds(), { padding: [20, 20] });
}

// ---------- Konto ----------
let me = null;
api('/api/web/me').then(u => {
  me = u;
  document.getElementById('whoami').textContent = u.username + (u.role === 'admin' ? ' (Admin)' : '');
  document.getElementById('nav-users').hidden = u.role !== 'admin';
});

const fmtDate = ts => ts ? new Date(ts).toLocaleString('de-AT', { dateStyle: 'short', timeStyle: 'short' }) : '–';

async function loadAccount() {
  const devs = await api('/api/web/me/devices');
  const el = document.getElementById('device-list');
  if (!devs.length) { el.innerHTML = '<p class="muted">Noch kein Handy angemeldet.</p>'; return; }
  el.innerHTML = `<table class="list"><tr><th>Handy</th><th class="hide-sm">angemeldet</th><th>zuletzt aktiv</th><th></th></tr>` +
    devs.map(d => `<tr><td>${esc(d.name)}</td><td class="hide-sm">${fmtDate(d.created)}</td><td>${fmtDate(d.lastSeen)}</td>
      <td class="actions-cell"><button data-dev="${d.id}">Entfernen</button></td></tr>`).join('') + '</table>';
  el.querySelectorAll('[data-dev]').forEach(btn => btn.onclick = async () => {
    if (!confirm('Dieses Handy abmelden? Es muss sich danach neu anmelden.')) return;
    await api('/api/web/me/devices/' + btn.dataset.dev, { method: 'DELETE' }).catch(e => alert(e.message));
    loadAccount();
  });
}

document.getElementById('pw-form').onsubmit = async e => {
  e.preventDefault();
  const f = e.target, msg = document.getElementById('pw-msg');
  if (f.new.value !== f.new2.value) { msg.className = 'error'; msg.textContent = 'Die neuen Passwörter stimmen nicht überein.'; return; }
  try {
    await api('/api/web/me/password', { method: 'POST', body: JSON.stringify({ current: f.current.value, new: f.new.value }) });
    f.reset(); msg.className = 'ok'; msg.textContent = 'Passwort geändert. Andere Browser wurden abgemeldet.';
  } catch (err) { msg.className = 'error'; msg.textContent = err.message.trim(); }
};

// ---------- Benutzer (Admin) ----------
async function loadUsers() {
  const [users, invites] = await Promise.all([api('/api/web/admin/users'), api('/api/web/admin/invites')]);

  const il = document.getElementById('invite-list');
  il.innerHTML = invites.length ? `<table class="list"><tr><th>Offener Link</th><th>gültig bis</th><th></th></tr>` +
    invites.map(i => `<tr><td>${i.purpose === 'reset' ? 'Neues Passwort: ' + esc(i.target) : 'Einladung'}${i.note && i.purpose !== 'reset' ? ' – ' + esc(i.note) : ''}</td>
      <td>${fmtDate(i.expires)}</td><td class="actions-cell"><button data-inv="${i.id}">Widerrufen</button></td></tr>`).join('') + '</table>' : '';
  il.querySelectorAll('[data-inv]').forEach(btn => btn.onclick = async () => {
    await api('/api/web/admin/invites/' + btn.dataset.inv, { method: 'DELETE' }); loadUsers();
  });

  const ul = document.getElementById('user-list');
  ul.innerHTML = `<table class="list"><tr><th>Benutzer</th><th class="hide-sm">seit</th><th>zuletzt aktiv</th><th class="hide-sm">Handys</th><th></th></tr>` +
    users.map(u => {
      const self = me && u.id === me.id;
      return `<tr><td>${esc(u.username)} ${u.role === 'admin' ? '<span class="tag">Admin</span>' : ''} ${u.disabled ? '<span class="tag off">gesperrt</span>' : ''}</td>
      <td class="hide-sm">${fmtDate(u.created)}</td><td>${fmtDate(u.lastActive)}</td><td class="hide-sm">${u.devices}</td>
      <td class="actions-cell">${self ? '<span class="muted">du</span>' : `
        <button data-act="reset" data-id="${u.id}" data-name="${esc(u.username)}">Passwort-Link</button>
        <button data-act="${u.disabled ? 'enable' : 'disable'}" data-id="${u.id}" data-name="${esc(u.username)}">${u.disabled ? 'Entsperren' : 'Sperren'}</button>
        <button data-act="delete" data-id="${u.id}" data-name="${esc(u.username)}" class="danger">Löschen</button>`}</td></tr>`;
    }).join('') + '</table>';
  ul.querySelectorAll('[data-act]').forEach(btn => btn.onclick = () => userAction(btn.dataset.act, btn.dataset.id, btn.dataset.name));
}

async function userAction(act, id, name) {
  try {
    if (act === 'delete') {
      if (prompt(`Benutzer „${name}“ mit ALLEN Positionen und Touren endgültig löschen?\nZur Bestätigung den Benutzernamen eingeben:`) !== name) return;
      await api('/api/web/admin/users/' + id, { method: 'DELETE' });
    } else if (act === 'disable') {
      if (!confirm(`„${name}“ sperren? Anmeldung und Handy funktionieren dann sofort nicht mehr.`)) return;
      await api(`/api/web/admin/users/${id}/disable`, { method: 'POST' });
    } else if (act === 'enable') {
      await api(`/api/web/admin/users/${id}/enable`, { method: 'POST' });
    } else if (act === 'reset') {
      const r = await api(`/api/web/admin/users/${id}/reset`, { method: 'POST' });
      showLink(r.url);
    }
  } catch (e) { alert(e.message); }
  loadUsers();
}

function showLink(url) {
  document.getElementById('invite-result').hidden = false;
  const inp = document.getElementById('invite-url');
  inp.value = url; inp.select();
}

document.getElementById('invite-form').onsubmit = async e => {
  e.preventDefault();
  try {
    const r = await api('/api/web/admin/invites', { method: 'POST', body: JSON.stringify({ note: e.target.note.value }) });
    e.target.reset(); showLink(r.url); loadUsers();
  } catch (err) { alert(err.message); }
};
document.getElementById('invite-copy').onclick = async () => {
  const inp = document.getElementById('invite-url');
  try { await navigator.clipboard.writeText(inp.value); } catch { inp.select(); document.execCommand('copy'); }
  document.getElementById('invite-copy').textContent = 'Kopiert ✓';
  setTimeout(() => document.getElementById('invite-copy').textContent = 'Kopieren', 2000);
};

// ---------- Statistik ----------
const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'Europe/Vienna';
let statsSport = '', statsUnit = 'weeks', statsData = null;
const km1 = m => (m / 1000).toLocaleString('de-AT', { maximumFractionDigits: m >= 100000 ? 0 : 1 }) + ' km';
const hm = s => s >= 3600 ? `${Math.floor(s / 3600)}:${String(Math.floor(s / 60) % 60).padStart(2, '0')} h` : `${Math.floor(s / 60)} min`;
const touren = n => n + (n === 1 ? ' Tour' : ' Touren');

document.getElementById('stats-xlsx').href = '/api/web/stats.xlsx?tz=' + encodeURIComponent(tz);
document.querySelectorAll('#stats-sport button').forEach(b => b.onclick = () => {
  statsSport = b.dataset.sport;
  document.querySelectorAll('#stats-sport button').forEach(x => x.classList.toggle('active', x === b));
  loadStats();
});
document.querySelectorAll('#stats-unit button').forEach(b => b.onclick = () => {
  statsUnit = b.dataset.unit;
  document.querySelectorAll('#stats-unit button').forEach(x => x.classList.toggle('active', x === b));
  renderChart();
});

async function loadStats() {
  statsData = await api(`/api/web/stats?tz=${encodeURIComponent(tz)}&sport=${statsSport}`);
  const d = statsData, run = statsSport === 'run';
  document.getElementById('stats-tiles').innerHTML = [['Diese Woche', d.week], ['Dieser Monat', d.month], ['Dieses Jahr', d.year], ['Gesamt', d.total]]
    .map(([l, t]) => `<div class="tile"><div class="t-label">${l}</div><div class="t-value">${km1(t.distanceM)}</div>
      <div class="t-sub">${touren(t.count)} · ${hm(t.movingS)}</div></div>`).join('');
  document.getElementById('stats-avg').innerHTML = d.total.count ? `
    <dt>pro Woche</dt><dd>${km1(d.perWeekM)}</dd>
    <dt>pro Tour</dt><dd>${km1(d.perTourM)}</dd>
    <dt>${run ? 'Ø Tempo' : 'Ø Geschwindigkeit'}</dt><dd>${run ? pace(d.total.distanceM, d.total.movingS) : (d.avgSpeed * 3.6).toFixed(1).replace('.', ',') + ' km/h'}</dd>
    <dt>Höhenmeter gesamt</dt><dd>${Math.round(d.total.elevGain).toLocaleString('de-AT')} m</dd>` : '<dd class="muted">Noch keine Touren.</dd>';
  const r = d.records, recs = [];
  if (r.longest) recs.push(['Längste', r.longest, km1(r.longest.distanceM)]);
  if (r.fastest) recs.push(['Schnellste (Ø)', r.fastest, isRun(r.fastest) ? pace(r.fastest.distanceM, r.fastest.movingS)
    : (r.fastest.distanceM / r.fastest.movingS * 3.6).toFixed(1).replace('.', ',') + ' km/h']);
  if (r.mostClimb) recs.push(['Meiste Höhenmeter', r.mostClimb, Math.round(r.mostClimb.elevGain) + ' m']);
  const rl = document.getElementById('stats-records');
  rl.innerHTML = recs.length ? recs.map(([l, t, v]) => `<div class="rec"><div><div class="r-sub">${l}</div>
      <a href="#" data-tour="${t.id}">${esc(t.name)}</a> <span class="badge ${isRun(t) ? 'run' : 'bike'}">${sportLabel(t)}</span></div>
      <div class="r-val">${v}</div></div>`).join('') : '<p class="muted">Noch keine Touren.</p>';
  rl.querySelectorAll('[data-tour]').forEach(a => a.onclick = async e => {
    e.preventDefault();
    document.querySelector('nav button[data-tab=tours]').click();
    await loadTours(); showTour(a.dataset.tour);
  });
  renderChart();
}

/** Säulendiagramm, eine Reihe (km je Zeitraum); Tooltip beim Überfahren. */
function renderChart() {
  if (!statsData) return;
  const periods = statsData[statsUnit];
  document.getElementById('chart-title').textContent = statsUnit === 'weeks' ? 'Kilometer pro Woche' : 'Kilometer pro Monat';
  const el = document.getElementById('chart'); el.textContent = '';
  // In echter Pixelbreite zeichnen, damit die Schrift auch am Handy lesbar bleibt.
  const W = Math.max(300, Math.round(el.clientWidth || 640)), H = W < 500 ? 200 : 220, L = 36, R = 8, T = 18, B = 34, n = periods.length;
  const maxKm = Math.max(0, ...periods.map(p => p.totals.distanceM / 1000));
  const step = maxKm <= 0 ? 10 : [1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000].find(s => maxKm / s <= 4) || 1000;
  const top = Math.max(step, Math.ceil(maxKm / step) * step);
  const x0 = i => L + i * (W - L - R) / n, bw = (W - L - R) / n;
  const y = km => T + (H - T - B) * (1 - km / top);
  const NS = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', (statsUnit === 'weeks' ? 'Kilometer pro Woche' : 'Kilometer pro Monat') + ', letzte 12');
  const add = (tag, attrs, text) => {
    const e = document.createElementNS(NS, tag);
    for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
    if (text != null) e.textContent = text;
    svg.appendChild(e); return e;
  };
  for (let v = 0; v <= top; v += step) {
    add('line', { x1: L, x2: W - R, y1: y(v), y2: y(v), class: 'grid' });
    add('text', { x: L - 6, y: y(v) + 4, 'text-anchor': 'end', class: 'axis' }, v);
  }
  const tip = document.createElement('div'); tip.className = 'chart-tip'; tip.hidden = true;
  const maxIdx = periods.findIndex(p => p.totals.distanceM / 1000 === maxKm && maxKm > 0);
  periods.forEach((p, i) => {
    const kmv = p.totals.distanceM / 1000, gap = Math.max(2, bw * 0.28);
    const bx = x0(i) + gap / 2, bwid = bw - gap, by = y(kmv), bh = (H - B) - by;
    let bar = null;
    if (kmv > 0) {
      const r = Math.min(4, bwid / 2, bh);
      // oben abgerundet, unten an der Grundlinie gerade
      bar = add('path', { class: 'bar', d: `M${bx},${H - B} V${by + r} Q${bx},${by} ${bx + r},${by} H${bx + bwid - r} Q${bx + bwid},${by} ${bx + bwid},${by + r} V${H - B} Z` });
    }
    if (i === maxIdx) add('text', { x: bx + bwid / 2, y: by - 5, 'text-anchor': 'middle', class: 'val' }, Math.round(kmv));
    if ((n - 1 - i) % 2 === 0) add('text', { x: bx + bwid / 2, y: H - B + 16, 'text-anchor': 'middle', class: 'axis' }, p.short);
    const hit = add('rect', { x: x0(i), y: T, width: bw, height: H - T - B, class: 'hit' });
    const show = () => {
      if (bar) bar.classList.add('hover');
      tip.textContent = `${p.label}: ${km1(p.totals.distanceM)} · ${touren(p.totals.count)}`;
      const box = el.getBoundingClientRect(), s = box.width / W;
      tip.style.left = Math.min(Math.max((bx + bwid / 2) * s, 70), box.width - 70) + 'px';
      tip.style.top = Math.min(by, H - B - 10) * s + 'px';
      tip.hidden = false;
    };
    const hide = () => { if (bar) bar.classList.remove('hover'); tip.hidden = true; };
    hit.addEventListener('mouseenter', show); hit.addEventListener('mouseleave', hide);
    hit.addEventListener('click', show);
  });
  svg.setAttribute('width', W); svg.setAttribute('height', H);
  if (statsUnit === 'weeks') add('text', { x: (L + W - R) / 2, y: H - 2, 'text-anchor': 'middle', class: 'axis' }, 'Kalenderwoche');
  el.append(svg, tip);
}

let chartResize;
window.addEventListener('resize', () => {
  clearTimeout(chartResize);
  chartResize = setTimeout(() => { if (!document.getElementById('tab-stats').hidden) renderChart(); }, 150);
});
