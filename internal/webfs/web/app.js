// railsignal operator console — native JS, no build step.
// Talks to the engine's HTTP API and renders the live interlocking state.

function el(id) { return document.getElementById(id); }
function out(msg) {
  const o = el('out');
  o.textContent = (msg == null ? '' : (typeof msg === 'string' ? msg : JSON.stringify(msg, null, 2))) + '\n' + o.textContent;
}

async function api(method, path, body) {
  const opt = { method, headers: {} };
  if (body != null) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  const r = await fetch(path, opt);
  const text = await r.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch (e) { data = text; }
  if (!r.ok) {
    out(`[ERR] ${method} ${path} ${r.status} ${text}`);
    throw new Error(`${path}: ${r.status} ${text}`);
  }
  return data;
}

async function refresh() {
  try {
    const layout = await api('GET', '/layout', null);
    el('clock').textContent = 'clock ' + layout.clock + 's';
    renderSections(layout.sections || []);
    renderPoints(layout.points || []);
    renderSignals(layout.signals || []);
    const routes = await api('GET', '/routes', null);
    renderRoutes(routes || []);
    const events = await api('GET', '/events?limit=20', null);
    renderEvents((events && events.events) || []);
  } catch (e) { /* errors logged via api */ }
}

function setTable(id, rows) {
  const tb = el(id).querySelector('tbody');
  tb.innerHTML = '';
  for (const r of rows) {
    const tr = document.createElement('tr');
    tr.innerHTML = r;
    tb.appendChild(tr);
  }
}

function renderSections(xs) {
  setTable('sections', xs.map(s => `<td>${s.code}</td><td class="${s.occupancy_count > 0 ? 'busy' : ''}">${s.occupancy_count}</td><td>${s.locked_by_route || ''}</td>`));
}
function renderPoints(xs) {
  setTable('points', xs.map(p => `<td>${p.code}</td><td>${p.direction}${p.bypassed ? '*' : ''}</td><td class="${p.status === 'FAULT' || p.status === 'OUT_OF_CORRESPONDENCE' ? 'busy' : ''}">${p.status}</td>`));
}
function renderSignals(xs) {
  setTable('signals', xs.map(s => `<td>${s.code}</td><td class="asp-${s.aspect.toLowerCase().replace('_', '')}">${s.aspect}</td><td>${s.route_id || ''}</td>`));
}
function renderRoutes(xs) {
  setTable('routes', xs.map(r => `<td>${r.code}</td><td>${r.state}</td><td>${r.released_count}/${(r.path_sections || []).length}</td>`));
}
function renderEvents(xs) {
  setTable('events', xs.map(e => `<td>${e.seq}</td><td>${e.kind}</td><td>${e.clock}</td><td>${truncate(e.payload)}</td>`));
}
function truncate(s) {
  if (s == null) return '';
  s = String(s);
  return s.length > 80 ? s.slice(0, 80) + '…' : s;
}

function bind(id, fn) { el(id).addEventListener('click', fn); }

bind('refresh', refresh);
bind('reconcile', async () => { try { await api('POST', '/reconcile', {}); out('reconciled'); refresh(); } catch (e) {} });
bind('add-node', async () => { try { await api('POST', '/nodes', { code: el('node-code').value }); refresh(); } catch (e) {} });
bind('add-section', async () => {
  try {
    await api('POST', '/sections', {
      code: el('sec-code').value, length_m: +el('sec-len').value,
      kind: el('sec-kind').value, from_node_id: el('sec-from').value, to_node_id: el('sec-to').value,
    });
    refresh();
  } catch (e) {}
});
bind('add-point', async () => {
  try {
    await api('POST', '/points', {
      code: el('pt-code').value, node_id: el('pt-node').value,
      heel_section_id: el('pt-heel').value, normal_section_id: el('pt-normal').value, reverse_section_id: el('pt-reverse').value,
      direction: 'N', max_move_seconds: +el('pt-max').value || 5,
    });
    refresh();
  } catch (e) {}
});
bind('add-signal', async () => {
  try {
    await api('POST', '/signals', { code: el('sig-code').value, entry_node_id: el('sig-entry').value, guard_section_id: el('sig-guard').value });
    refresh();
  } catch (e) {}
});
bind('request-route', async () => {
  try {
    const r = await api('POST', '/routes', { origin_signal_id: el('route-origin').value, terminal_section_id: el('route-terminal').value });
    out(r); refresh();
  } catch (e) {}
});
bind('occupancy', async () => { try { const r = await api('POST', '/occupancy', { section_id: el('occ-sec').value }); out(r); refresh(); } catch (e) {} });
bind('clearance', async () => { try { const r = await api('POST', '/clearance', { section_id: el('clr-sec').value }); out(r); refresh(); } catch (e) {} });
bind('cancel-route', async () => { try { const r = await api('POST', '/routes/' + el('cancel-rt').value + '/cancel', {}); out(r); refresh(); } catch (e) {} });
bind('advance-clock', async () => { try { const r = await api('POST', '/clock/advance', { delta: +el('clk-delta').value || 1 }); out(r); refresh(); } catch (e) {} });
bind('bypass-on', async () => { try { await api('POST', '/points/' + el('bypass-pt').value + '/bypass', { on: true }); refresh(); } catch (e) {} });
bind('bypass-off', async () => { try { await api('POST', '/points/' + el('bypass-pt').value + '/bypass', { on: false }); refresh(); } catch (e) {} });

refresh();
