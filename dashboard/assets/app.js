import { html, render, useState, useEffect, useRef } from './vendor/preact-htm.js';

const HISTORY_STEP_SECONDS = 5; // the server keeps one hour at one sample every 5 seconds
const MAX_LOG_EVENTS = 1000;

// ---------------------------------------------------------------- API

async function request(method, path, body) {
  const res = await fetch('/dashboard/api' + path, {
    method,
    cache: 'no-store',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-WSBro-Dashboard': '1' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || 'HTTP ' + res.status);
  return data;
}
const api = {
  get: (path) => request('GET', path),
  post: (path, body = {}) => request('POST', path, body),
};

// ---------------------------------------------------------------- Formatting

const numberFormat = new Intl.NumberFormat();
const num = (n) => numberFormat.format(Math.round(n || 0));
const compact = (n) => new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(n || 0);

function bytes(n) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return (i ? n.toFixed(1) : n) + ' ' + units[i];
}

function duration(seconds) {
  seconds = Math.max(0, Math.floor(seconds));
  const d = Math.floor(seconds / 86400), h = Math.floor(seconds % 86400 / 3600), m = Math.floor(seconds % 3600 / 60), s = seconds % 60;
  if (d) return d + 'd ' + h + 'h';
  if (h) return h + 'h ' + m + 'm';
  if (m) return m + 'm ' + s + 's';
  return s + 's';
}

const ago = (ms) => duration((Date.now() - ms) / 1000);
const clock = (ms) => new Date(ms).toLocaleTimeString([], { hour12: false }) + '.' + String(ms % 1000).padStart(3, '0');

function prettyJSON(text) {
  try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return text; }
}

function browser(userAgent) {
  if (!userAgent) return '—';
  const known = [['Edg/', 'Edge'], ['OPR/', 'Opera'], ['Firefox/', 'Firefox'], ['Chrome/', 'Chrome'], ['Safari/', 'Safari'], ['okhttp', 'Android app'], ['Go-http-client', 'Go client'], ['node', 'Node.js']];
  const found = known.find(([needle]) => userAgent.includes(needle));
  const os = /Windows/.test(userAgent) ? 'Windows' : /Mac OS/.test(userAgent) ? 'macOS' : /Android/.test(userAgent) ? 'Android' : /iPhone|iPad/.test(userAgent) ? 'iOS' : /Linux/.test(userAgent) ? 'Linux' : '';
  return [found ? found[1] : userAgent.split(/[ /]/)[0], os].filter(Boolean).join(' · ');
}

// ---------------------------------------------------------------- Icons

const ICONS = {
  overview: 'M3 13h8V3H3zM13 21h8V11h-8zM3 21h8v-6H3zM13 3v6h8V3z',
  channels: 'M4 9h16M4 15h16M10 3 8 21M16 3l-2 18',
  connections: 'M5 12.55a11 11 0 0 1 14.08 0M1.42 9a16 16 0 0 1 21.16 0M8.53 16.11a6 6 0 0 1 6.95 0M12 20h.01',
  console: 'm4 17 6-6-6-6M12 19h8',
  apps: 'M12 2 2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5',
  users: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75',
  pulse: 'M22 12h-4l-3 9L9 3l-3 9H2',
  send: 'm22 2-7 20-4-9-9-4zM22 2 11 13',
  clock: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20zM12 6v6l4 2',
  x: 'M18 6 6 18M6 6l12 12',
  pause: 'M6 4h4v16H6zM14 4h4v16h-4z',
  play: 'm6 3 14 9-14 9z',
  trash: 'M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6',
  sun: 'M12 17a5 5 0 1 0 0-10 5 5 0 0 0 0 10zM12 1v2M12 21v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M1 12h2M21 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4',
  moon: 'M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z',
  monitor: 'M2 3h20v14H2zM8 21h8M12 17v4',
  inbox: 'M22 12h-6l-2 3h-4l-2-3H2M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z',
  server: 'M2 2h20v8H2zM2 14h20v8H2zM6 6h.01M6 18h.01',
  copy: 'M9 9h13v13H9zM5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1',
};

const Icon = ({ name }) => html`
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d=${ICONS[name]} />
  </svg>`;

// ---------------------------------------------------------------- Hooks

function useInterval(callback, delay, deps = []) {
  const saved = useRef(callback);
  saved.current = callback;
  useEffect(() => {
    saved.current();
    const id = setInterval(() => saved.current(), delay);
    return () => clearInterval(id);
  }, deps);
}

function useDebounced(value, delay = 250) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(id);
  }, [value]);
  return debounced;
}

function useStored(key, initial) {
  const [value, setValue] = useState(() => {
    try { return JSON.parse(localStorage.getItem(key)) ?? initial; } catch { return initial; }
  });
  useEffect(() => { try { localStorage.setItem(key, JSON.stringify(value)); } catch {} }, [value]);
  return [value, setValue];
}

function useRoute() {
  const read = () => location.hash.replace(/^#\/?/, '') || 'overview';
  const [route, setRoute] = useState(read);
  useEffect(() => {
    const onChange = () => setRoute(read());
    addEventListener('hashchange', onChange);
    return () => removeEventListener('hashchange', onChange);
  }, []);
  return route;
}

function useTheme() {
  const [mode, setMode] = useStored('wsbro.theme', 'auto');
  useEffect(() => {
    const media = matchMedia('(prefers-color-scheme: dark)');
    const apply = () => {
      document.documentElement.dataset.theme = mode === 'auto' ? (media.matches ? 'dark' : 'light') : mode;
    };
    apply();
    media.addEventListener('change', apply);
    return () => media.removeEventListener('change', apply);
  }, [mode]);
  return [mode, setMode];
}

// useDebugStream keeps one Server-Sent Events connection for an app, optionally for one channel only.
// Incoming events are buffered and flushed a few times per second to keep rendering cheap.
function useDebugStream(appId, channel = '') {
  const [events, setEvents] = useState([]);
  const [connected, setConnected] = useState(false);
  const [paused, setPaused] = useState(false);
  const buffer = useRef([]);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  useEffect(() => {
    if (!appId) return;
    setEvents([]);
    const source = new EventSource('/dashboard/api/stream?' + new URLSearchParams({ app: appId, channel }));
    source.onopen = () => setConnected(true);
    source.onerror = () => setConnected(false);
    source.onmessage = (e) => { if (!pausedRef.current) buffer.current.push(JSON.parse(e.data)); };

    const flush = setInterval(() => {
      if (buffer.current.length === 0) return;
      const fresh = buffer.current.reverse();
      buffer.current = [];
      setEvents((old) => fresh.concat(old).slice(0, MAX_LOG_EVENTS));
    }, 300);

    return () => { source.close(); clearInterval(flush); };
  }, [appId, channel]);

  return { events, connected, paused, setPaused, clear: () => setEvents([]) };
}

// ---------------------------------------------------------------- Shared components

function Toast({ toast }) {
  if (!toast) return null;
  return html`<div class=${'toast ' + (toast.error ? 'error' : '')} role="status">${toast.text}</div>`;
}

function Empty({ icon = 'inbox', children }) {
  return html`<div class="empty"><${Icon} name=${icon} />${children}</div>`;
}

function Pagination({ page, perPage, total, onPage, onPerPage }) {
  const pages = Math.max(1, Math.ceil(total / perPage));
  const from = total === 0 ? 0 : (page - 1) * perPage + 1;
  const to = Math.min(page * perPage, total);

  const numbers = [];
  for (let p = Math.max(1, page - 2); p <= Math.min(pages, page + 2); p++) numbers.push(p);
  if (numbers[0] > 1) numbers.unshift(1, '…');
  if (numbers[numbers.length - 1] < pages) numbers.push('…', pages);

  return html`
    <div class="pager">
      <span>${num(from)}–${num(to)} of ${num(total)}</span>
      <select class="select" value=${perPage} onChange=${(e) => onPerPage(Number(e.target.value))} aria-label="Rows per page">
        ${[10, 25, 50, 100, 200].map((n) => html`<option value=${n}>${n} / page</option>`)}
      </select>
      <span class="spacer"></span>
      <div class="pages">
        <button disabled=${page <= 1} onClick=${() => onPage(page - 1)} aria-label="Previous page">‹</button>
        ${numbers.map((p, i) => p === '…'
          ? html`<button key=${'gap' + i} disabled>…</button>`
          : html`<button key=${p} class=${p === page ? 'active' : ''} onClick=${() => onPage(p)}>${p}</button>`)}
        <button disabled=${page >= pages} onClick=${() => onPage(page + 1)} aria-label="Next page">›</button>
      </div>
    </div>`;
}

function TypeBadge({ type }) {
  return html`<span class=${'badge ' + type}>${type}</span>`;
}

const RANGES = [['5m', 60, '5 min'], ['15m', 180, '15 min'], ['1h', 720, '1 hour']];

function Chart({ title, values, points: slots, color, format = num, suffix = '' }) {
  const [hover, setHover] = useState(null);
  const W = 600, H = 120;
  values = values.slice(-slots);
  const max = Math.max(1, ...values);
  const step = W / (slots - 1);
  const offset = (slots - values.length) * step;
  const points = values.map((v, i) => [offset + i * step, H - (v / max) * (H - 8)]);
  const line = points.map(([x, y]) => x.toFixed(1) + ',' + y.toFixed(1)).join(' ');
  const area = points.length > 1 ? 'M' + points[0][0] + ',' + H + ' L' + line.replaceAll(' ', ' L') + ' L' + points[points.length - 1][0] + ',' + H + ' Z' : '';
  const current = values.length ? values[values.length - 1] : 0;
  const id = 'g-' + title.replace(/\W/g, '');

  const onMove = (e) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = ((e.clientX - rect.left) / rect.width) * W;
    const i = Math.round((x - offset) / step);
    if (i >= 0 && i < values.length) setHover({ i, left: (points[i][0] / W) * 100 });
    else setHover(null);
  };

  return html`
    <div class="card chart-card">
      <div class="chart-head">
        <span class="title">${title}</span>
        <span class="now">${format(current)}${suffix}</span>
      </div>
      <div class="chart" onMouseMove=${onMove} onMouseLeave=${() => setHover(null)}>
        <svg viewBox=${'0 0 ' + W + ' ' + H} preserveAspectRatio="none" role="img" aria-label=${title}>
          <defs>
            <linearGradient id=${id} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" stop-color=${color} stop-opacity=".28" />
              <stop offset="1" stop-color=${color} stop-opacity="0" />
            </linearGradient>
          </defs>
          ${[0, 0.5, 1].map((f) => html`<line class="grid-line" x1="0" x2=${W} y1=${H - f * (H - 8)} y2=${H - f * (H - 8)} vector-effect="non-scaling-stroke" />`)}
          ${area && html`<path d=${area} fill=${'url(#' + id + ')'} />`}
          ${line && html`<polyline points=${line} fill="none" stroke=${color} stroke-width="2" stroke-linejoin="round" vector-effect="non-scaling-stroke" />`}
          ${hover && html`<line class="cursor" x1=${points[hover.i][0]} x2=${points[hover.i][0]} y1="0" y2=${H} vector-effect="non-scaling-stroke" />`}
        </svg>
        ${hover && html`<div class="tooltip" style=${'left:' + hover.left + '%'}>${format(values[hover.i])}${suffix} · ${duration((values.length - 1 - hover.i) * HISTORY_STEP_SECONDS)} ago</div>`}
      </div>
      <div class="row small faint"><span>max ${format(max)}${suffix}</span></div>
    </div>`;
}

// ---------------------------------------------------------------- Overview

function Overview({ data, history, rates, app }) {
  const s = data.stats;
  const last = rates;
  const server = data.server;
  const [range, setRange] = useStored('wsbro.range', '5m');
  const slots = (RANGES.find(([id]) => id === range) || RANGES[0])[1];

  return html`
    <div class="cards">
      <div class="card stat">
        <div class="label"><${Icon} name="connections" />Connections</div>
        <div class="value">${num(s.connections)}</div>
        <div class="sub">peak ${num(s.peak_connections)}</div>
      </div>
      <div class="card stat">
        <div class="label"><${Icon} name="channels" />Channels</div>
        <div class="value">${num(s.channels)}</div>
        <div class="sub">across ${s.apps.length} ${s.apps.length === 1 ? 'app' : 'apps'}</div>
      </div>
      <div class="card stat">
        <div class="label"><${Icon} name="pulse" />Messages / sec</div>
        <div class="value">${num(last.msgRate)}</div>
        <div class="sub">${compact(s.total_messages)} delivered</div>
      </div>
      <div class="card stat">
        <div class="label"><${Icon} name="send" />API events / sec</div>
        <div class="value">${num(last.eventRate)}</div>
        <div class="sub">${compact(s.total_events)} received</div>
      </div>
      <div class="card stat">
        <div class="label"><${Icon} name="clock" />Uptime</div>
        <div class="value">${duration(s.uptime_seconds)}</div>
        <div class="sub">${bytes(server.memory_bytes)} RAM · ${num(server.goroutines)} goroutines</div>
      </div>
    </div>

    <div class="section-head">
      <h2>Activity</h2>
      <span class="spacer"></span>
      <div class="segmented" role="group" aria-label="Time range">
        ${RANGES.map(([id, , label]) => html`<button class=${range === id ? 'active' : ''} onClick=${() => setRange(id)}>${label}</button>`)}
      </div>
    </div>
    <div class="grid-3">
      <${Chart} title="Connections" points=${slots} values=${history.map((p) => p.connections)} color="var(--accent)" />
      <${Chart} title="Messages delivered / sec" points=${slots} values=${history.map((p) => p.message_rate)} color="var(--accent-2)" />
      <${Chart} title="API events / sec" points=${slots} values=${history.map((p) => p.event_rate)} color="var(--accent-3)" />
    </div>

    <div class="card panel">
      <div class="panel-head"><h2>Apps</h2><span class="hint">Live usage per application</span></div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>App</th><th>Key</th><th class="num">Connections</th><th class="num">Channels</th><th class="num">Limit</th><th class="hide-sm">Load</th></tr></thead>
          <tbody>
            ${data.apps.map((a) => {
              const limit = a.max_connections || server.max_connections;
              const load = limit ? Math.min(100, (a.connections / limit) * 100) : 0;
              return html`
                <tr key=${a.id} class=${a.id === app ? 'selected' : ''}>
                  <td><b>${a.id}</b></td>
                  <td class="mono muted">${a.key}</td>
                  <td class="num">${num(a.connections)}</td>
                  <td class="num">${num(a.channels)}</td>
                  <td class="num muted">${limit ? num(limit) : '∞'}</td>
                  <td class="hide-sm" style="width:180px">${limit ? html`<${Meter} value=${load} />` : html`<span class="faint small">no limit</span>`}</td>
                </tr>`;
            })}
          </tbody>
        </table>
      </div>
    </div>`;
}

function Meter({ value }) {
  const color = value > 90 ? 'var(--danger)' : value > 70 ? 'var(--warn)' : 'var(--accent)';
  return html`
    <div style="display:flex;align-items:center;gap:8px">
      <div style="flex:1;height:6px;border-radius:9px;background:var(--panel-2);border:1px solid var(--border);overflow:hidden">
        <div style=${'height:100%;width:' + value + '%;background:' + color}></div>
      </div>
      <span class="small muted" style="width:36px;text-align:right">${Math.round(value)}%</span>
    </div>`;
}

// ---------------------------------------------------------------- Channels

function Channels({ app, notify }) {
  const [search, setSearch] = useState('');
  const [type, setType] = useState('');
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useStored('wsbro.perPage', 25);
  const [result, setResult] = useState(null);
  const [selected, setSelected] = useState(null);
  const query = useDebounced(search);

  useEffect(() => setPage(1), [query, type, app, perPage]);

  useInterval(async () => {
    const params = new URLSearchParams({ search: query, type, page, per_page: perPage });
    try { setResult(await api.get('/apps/' + encodeURIComponent(app) + '/channels?' + params)); } catch {}
  }, 3000, [app, query, type, page, perPage]);

  return html`
    <div class="card panel">
      <div class="panel-head">
        <h2>Channels</h2>
        <span class="hint">${result ? num(result.total) + ' occupied' : ''}</span>
        <span class="spacer"></span>
        <div class="segmented" role="tablist">
          ${[['', 'All'], ['public', 'Public'], ['private', 'Private'], ['presence', 'Presence']].map(([value, label]) => html`
            <button class=${type === value ? 'active' : ''} onClick=${() => setType(value)}>${label}</button>`)}
        </div>
        <input class="input search" type="search" placeholder="Search channels…" value=${search} onInput=${(e) => setSearch(e.target.value)} aria-label="Search channels" />
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>Channel</th><th>Type</th><th class="num">Connections</th><th class="num">Users</th></tr></thead>
          <tbody>
            ${result && result.items.map((c) => html`
              <tr key=${c.name} class=${'clickable ' + (selected === c.name ? 'selected' : '')} onClick=${() => setSelected(c.name)}>
                <td class="mono truncate" title=${c.name}>${c.name}</td>
                <td><${TypeBadge} type=${c.type} /></td>
                <td class="num">${num(c.connections)}</td>
                <td class="num">${c.type === 'presence' ? num(c.users) : html`<span class="faint">—</span>`}</td>
              </tr>`)}
          </tbody>
        </table>
        ${result && result.total === 0 && html`<${Empty} icon="channels">${query || type ? 'No channels match the filter' : 'No occupied channels yet'}<//>`}
      </div>
      ${result && result.total > 0 && html`<${Pagination} page=${result.page} perPage=${perPage} total=${result.total} onPage=${setPage} onPerPage=${setPerPage} />`}
    </div>
    ${selected && html`<${ChannelDrawer} app=${app} name=${selected} notify=${notify} onClose=${() => setSelected(null)} />`}`;
}

function ChannelDrawer({ app, name, notify, onClose }) {
  const [info, setInfo] = useState(null);
  const [event, setEvent] = useState('test-event');
  const [data, setData] = useState('{\n  "message": "Hello from WSBro"\n}');
  const [sending, setSending] = useState(false);

  useInterval(async () => {
    try { setInfo(await api.get('/apps/' + encodeURIComponent(app) + '/channels/' + encodeURIComponent(name))); } catch {}
  }, 2000, [app, name]);

  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose();
    addEventListener('keydown', onKey);
    return () => removeEventListener('keydown', onKey);
  }, []);

  // A stream filtered on the server, so busy channels elsewhere never push these events out.
  const stream = useDebugStream(app, name);
  const recent = stream.events.slice(0, 50);

  const send = async (e) => {
    e.preventDefault();
    setSending(true);
    try {
      await api.post('/apps/' + encodeURIComponent(app) + '/events', { name: event, channel: name, data });
      notify('Event “' + event + '” sent to ' + name);
    } catch (err) {
      notify(err.message, true);
    } finally {
      setSending(false);
    }
  };

  const type = name.startsWith('presence-') ? 'presence' : name.startsWith('private-') ? 'private' : 'public';

  return html`
    <div class="drawer-backdrop" onClick=${onClose}></div>
    <aside class="drawer" role="dialog" aria-label=${'Channel ' + name}>
      <div class="drawer-head">
        <${TypeBadge} type=${type} />
        <h3 class="mono">${name}</h3>
        <span class="spacer"></span>
        <button class="btn icon" onClick=${onClose} aria-label="Close"><${Icon} name="x" /></button>
      </div>
      <div class="drawer-body">
        <div class="kv">
          <div><span>Status</span><strong class=${info && info.occupied ? 'ok-text' : 'faint'}>${info ? (info.occupied ? 'occupied' : 'empty') : '…'}</strong></div>
          <div><span>Connections</span><strong>${info ? num(info.connections) : '…'}</strong></div>
          <div><span>Users</span><strong>${info && type === 'presence' ? num(info.users) : '—'}</strong></div>
        </div>

        ${type === 'presence' && html`
          <div>
            <p class="section-title">Members${info && info.members_truncated ? ' (first 500)' : ''}</p>
            ${info && info.members.length > 0
              ? html`<div class="members">${info.members.map((m) => html`
                  <div class="member" key=${m.user_id}>
                    <span class="avatar">${String(m.user_id).slice(0, 2).toUpperCase()}</span>
                    <div style="min-width:0">
                      <div><b>${(m.user_info && (m.user_info.name || m.user_info.email)) || 'User ' + m.user_id}</b> <span class="faint small mono">#${m.user_id}</span></div>
                      ${m.user_info && html`<div class="mono small muted truncate">${JSON.stringify(m.user_info)}</div>`}
                    </div>
                  </div>`)}</div>`
              : html`<div class="muted small">Nobody here right now.</div>`}
          </div>`}

        <form onSubmit=${send}>
          <p class="section-title">Send a test event</p>
          <div style="display:flex;flex-direction:column;gap:10px">
            <label class="field">Event name<input class="input mono" value=${event} onInput=${(e) => setEvent(e.target.value)} required /></label>
            <label class="field">Data (JSON or text)<textarea class="mono" value=${data} onInput=${(e) => setData(e.target.value)}></textarea></label>
            <div class="row"><button class="btn primary" disabled=${sending}><${Icon} name="send" />${sending ? 'Sending…' : 'Send event'}</button>
              <span class="small muted">Delivered to every subscriber, like a broadcast from Laravel.</span></div>
          </div>
        </form>

        <div>
          <p class="section-title">Live activity</p>
          <div class="card log" style="overflow:hidden">
            ${recent.length === 0
              ? html`<div class="empty" style="padding:24px">Waiting for activity on this channel…</div>`
              : recent.map((e, i) => html`<${LogRow} key=${e.time + '-' + i} event=${e} compact />`)}
          </div>
        </div>
      </div>
    </aside>`;
}

// ---------------------------------------------------------------- Connections

function Connections({ app, notify }) {
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useStored('wsbro.perPage', 25);
  const [result, setResult] = useState(null);
  const [confirming, setConfirming] = useState(null);
  const [tick, setTick] = useState(0);
  const query = useDebounced(search);

  useEffect(() => setPage(1), [query, app, perPage]);
  useEffect(() => {
    if (!confirming) return;
    const id = setTimeout(() => setConfirming(null), 3000);
    return () => clearTimeout(id);
  }, [confirming]);

  useInterval(async () => {
    const params = new URLSearchParams({ search: query, page, per_page: perPage });
    try { setResult(await api.get('/apps/' + encodeURIComponent(app) + '/connections?' + params)); } catch {}
  }, 3000, [app, query, page, perPage, tick]);

  const disconnect = async (socketId) => {
    if (confirming !== socketId) return setConfirming(socketId);
    setConfirming(null);
    try {
      await api.post('/apps/' + encodeURIComponent(app) + '/connections/' + encodeURIComponent(socketId) + '/disconnect');
      notify('Connection ' + socketId + ' disconnected');
      setTick((t) => t + 1);
    } catch (err) {
      notify(err.message, true);
    }
  };

  return html`
    <div class="card panel">
      <div class="panel-head">
        <h2>Connections</h2>
        <span class="hint">${result ? num(result.total) + ' open' : ''}</span>
        <span class="spacer"></span>
        <input class="input search" type="search" placeholder="Socket ID, IP, user or browser…" value=${search} onInput=${(e) => setSearch(e.target.value)} aria-label="Search connections" />
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>Socket ID</th><th>User</th><th>Client</th><th class="hide-sm">IP</th><th class="num">Channels</th><th class="num">Connected</th><th></th></tr></thead>
          <tbody>
            ${result && result.items.map((c) => html`
              <tr key=${c.socket_id}>
                <td class="mono">${c.socket_id}</td>
                <td>${c.user_id ? html`<span class="badge info">#${c.user_id}</span>` : html`<span class="faint">guest</span>`}</td>
                <td class="truncate" title=${c.user_agent}>${browser(c.user_agent)}</td>
                <td class="mono muted hide-sm">${c.remote_addr}</td>
                <td class="num">${num(c.channels)}</td>
                <td class="num muted">${ago(c.connected_at)}</td>
                <td class="num">
                  <button class=${'btn small danger ' + (confirming === c.socket_id ? 'confirm' : '')} onClick=${() => disconnect(c.socket_id)}>
                    ${confirming === c.socket_id ? 'Confirm' : 'Disconnect'}
                  </button>
                </td>
              </tr>`)}
          </tbody>
        </table>
        ${result && result.total === 0 && html`<${Empty} icon="connections">${query ? 'No connections match the search' : 'No clients connected'}<//>`}
      </div>
      ${result && result.total > 0 && html`<${Pagination} page=${result.page} perPage=${perPage} total=${result.total} onPage=${setPage} onPerPage=${setPerPage} />`}
    </div>`;
}

// ---------------------------------------------------------------- Debug console

const LOG_TYPES = {
  connected: { label: 'connected', badge: 'ok', group: 'connections' },
  disconnected: { label: 'disconnected', badge: 'neutral', group: 'connections' },
  subscribed: { label: 'subscribed', badge: 'ok', group: 'subscriptions' },
  unsubscribed: { label: 'unsubscribed', badge: 'neutral', group: 'subscriptions' },
  member_added: { label: 'member added', badge: 'presence', group: 'subscriptions' },
  member_removed: { label: 'member left', badge: 'presence', group: 'subscriptions' },
  signin: { label: 'signed in', badge: 'info', group: 'subscriptions' },
  api_event: { label: 'API event', badge: 'info', group: 'events' },
  client_event: { label: 'client event', badge: 'private', group: 'events' },
  auth_failed: { label: 'auth failed', badge: 'bad', group: 'errors' },
  error: { label: 'error', badge: 'bad', group: 'errors' },
  dropped: { label: 'skipped', badge: 'bad', group: 'errors' },
};

const LOG_GROUPS = [['connections', 'Connections'], ['subscriptions', 'Subscriptions'], ['events', 'Events'], ['errors', 'Errors']];

function LogRow({ event, compact }) {
  const [open, setOpen] = useState(false);
  const meta = LOG_TYPES[event.type] || { label: event.type, badge: 'neutral' };
  return html`
    <div class="log-row" onClick=${() => setOpen(!open)}>
      <span class="time">${clock(event.time)}</span>
      <span class="type-col"><span class=${'badge ' + meta.badge}>${meta.label}</span></span>
      <span class="what">
        ${event.event && html`<b>${event.event}</b> `}
        ${event.channel && !compact && html`<span class="channel-name">${event.channel}</span> `}
        ${event.user_id && html`<span class="meta">user #${event.user_id} </span>`}
        ${event.socket_id && html`<span class="meta">${event.socket_id} </span>`}
        ${event.type === 'dropped'
          ? html`<span class="muted">${num(event.data)} events skipped: too many at once for the dashboard. Narrow it down with a channel.</span>`
          : !open && event.data && html`<span class="faint">${event.data}</span>`}
      </span>
      ${open && html`<div class="log-detail" onClick=${(e) => e.stopPropagation()}>${event.data ? prettyJSON(event.data) : 'No payload'}</div>`}
    </div>`;
}

function Console({ stream }) {
  const [groups, setGroups] = useStored('wsbro.logGroups', ['connections', 'subscriptions', 'events', 'errors']);
  const [search, setSearch] = useState('');
  const query = useDebounced(search, 150).toLowerCase();

  const toggle = (group) => setGroups(groups.includes(group) ? groups.filter((g) => g !== group) : groups.concat(group));
  const visible = stream.events.filter((e) => {
    const meta = LOG_TYPES[e.type];
    if (meta && !groups.includes(meta.group)) return false;
    if (!query) return true;
    return [e.channel, e.event, e.socket_id, e.user_id, e.data].some((v) => v && v.toLowerCase().includes(query));
  });

  return html`
    <div class="card panel">
      <div class="panel-head">
        <h2>Debug console</h2>
        <span class="pill"><span class=${'dot ' + (stream.connected && !stream.paused ? 'live' : 'off')}></span>${stream.paused ? 'Paused' : stream.connected ? 'Streaming' : 'Reconnecting'}</span>
        <span class="spacer"></span>
        <div class="chips">
          ${LOG_GROUPS.map(([group, label]) => html`<button class=${'chip ' + (groups.includes(group) ? 'active' : '')} onClick=${() => toggle(group)}>${label}</button>`)}
        </div>
        <input class="input search" type="search" placeholder="Filter by channel, event, socket, payload…" value=${search} onInput=${(e) => setSearch(e.target.value)} aria-label="Filter events" />
        <button class="btn" onClick=${() => stream.setPaused(!stream.paused)}><${Icon} name=${stream.paused ? 'play' : 'pause'} />${stream.paused ? 'Resume' : 'Pause'}</button>
        <button class="btn" onClick=${stream.clear}><${Icon} name="trash" />Clear</button>
      </div>
      <div class="log log-scroll">
        ${visible.length === 0
          ? html`<${Empty} icon="console">${stream.events.length ? 'No events match the filter' : 'Waiting for activity… Connect a client or send an event.'}<//>`
          : visible.slice(0, 500).map((e, i) => html`<${LogRow} key=${e.time + '-' + e.type + '-' + (e.socket_id || '') + '-' + i} event=${e} />`)}
      </div>
      <div class="pager"><span>${num(visible.length)} events shown · keeps the last ${num(MAX_LOG_EVENTS)}</span></div>
    </div>`;
}

// ---------------------------------------------------------------- Apps & server

function Apps({ data, app, notify }) {
  const current = data.apps.find((a) => a.id === app) || data.apps[0];
  const server = data.server;
  const host = location.hostname;
  const port = location.port || (location.protocol === 'https:' ? '443' : '80');
  const scheme = location.protocol === 'https:' ? 'https' : 'http';
  const env = [
    'BROADCAST_CONNECTION=reverb',
    'REVERB_APP_ID=' + current.id,
    'REVERB_APP_KEY=' + current.key,
    'REVERB_APP_SECRET=your-app-secret',
    'REVERB_HOST=' + host,
    'REVERB_PORT=' + port,
    'REVERB_SCHEME=' + scheme,
  ].join('\n');

  const copy = async () => {
    try { await navigator.clipboard.writeText(env); notify('Copied to clipboard'); } catch { notify('Copy failed', true); }
  };

  const rows = [
    ['Node ID', html`<span class="mono">${server.node_id}</span>`],
    ['Version', html`<span class="mono">${data.stats.version}</span>`],
    ['Go', html`<span class="mono">${server.go_version}</span>`],
    ['Adapter', data.stats.adapter],
    ['TLS', server.tls ? html`<span class="badge ok">enabled</span>` : html`<span class="badge neutral">off (use a proxy)</span>`],
    ['Started', new Date(server.started_at).toLocaleString()],
    ['Memory', bytes(server.memory_bytes)],
    ['Goroutines', num(server.goroutines)],
    ['Max connections', server.max_connections ? num(server.max_connections) : 'unlimited'],
    ['Max channels per connection', num(server.max_channels_per_connection)],
    ['Max message size', bytes(server.max_message_size)],
    ['Max event payload', bytes(server.max_event_data_size)],
  ];

  return html`
    <div class="card panel">
      <div class="panel-head"><h2>Applications</h2><span class="hint">Configured with WSBRO_APPS or WSBRO_APP_*. Secrets are never shown.</span></div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>ID</th><th>Key</th><th class="num">Max connections</th><th class="num">Messages / sec</th><th>Allowed origins</th><th>Webhook</th></tr></thead>
          <tbody>
            ${data.apps.map((a) => html`
              <tr key=${a.id} class=${a.id === app ? 'selected' : ''}>
                <td><b>${a.id}</b></td>
                <td class="mono">${a.key}</td>
                <td class="num">${a.max_connections ? num(a.max_connections) : html`<span class="faint">server limit</span>`}</td>
                <td class="num">${a.max_messages_per_second ? num(a.max_messages_per_second) : '∞'}</td>
                <td class="mono small">${a.allowed_origins || '*'}</td>
                <td class="mono small truncate">${a.webhook_url || html`<span class="faint">—</span>`}</td>
              </tr>`)}
          </tbody>
        </table>
      </div>
    </div>

    <div class="grid-2">
      <div class="card panel" style="margin:0">
        <div class="panel-head"><h2>Connect Laravel</h2><span class="hint">app “${current.id}”</span><span class="spacer"></span>
          <button class="btn small" onClick=${copy}><${Icon} name="copy" />Copy</button></div>
        <div class="panel-body">
          <pre class="mono" style="margin:0;white-space:pre-wrap">${env}</pre>
          <p class="small muted" style="margin:12px 0 0">Works with <span class="mono">php artisan install:broadcasting</span> out of the box: WSBro speaks the same protocol as Reverb. Run WSBro instead of <span class="mono">reverb:start</span>.</p>
        </div>
      </div>
      <div class="card panel" style="margin:0">
        <div class="panel-head"><h2>Server</h2></div>
        <table><tbody>
          ${rows.map(([k, v]) => html`<tr><td class="muted">${k}</td><td class="num">${v}</td></tr>`)}
        </tbody></table>
      </div>
    </div>`;
}

// ---------------------------------------------------------------- App shell

const VIEWS = [
  ['overview', 'Overview', 'overview'],
  ['channels', 'Channels', 'channels'],
  ['connections', 'Connections', 'connections'],
  ['console', 'Debug console', 'console'],
  ['apps', 'Apps & server', 'apps'],
];

function App() {
  const route = useRoute();
  const [theme, setTheme] = useTheme();
  const [data, setData] = useState(null);
  const [online, setOnline] = useState(true);
  const [history, setHistory] = useState([]);
  const [rates, setRates] = useState({ msgRate: 0, eventRate: 0 });
  const [app, setApp] = useStored('wsbro.app', null);
  const [toast, setToast] = useState(null);
  const previous = useRef(null);
  const stream = useDebugStream(app);

  const notify = (text, error = false) => {
    setToast({ text, error });
    clearTimeout(notify.timer);
    notify.timer = setTimeout(() => setToast(null), 3000);
  };

  useInterval(async () => {
    try {
      const next = await api.get('/stats');
      const now = Date.now(), prev = previous.current;
      const seconds = prev ? (now - prev.time) / 1000 : 0;
      const rate = (key) => (prev && seconds > 0 ? Math.max(0, (next.stats[key] - prev.stats[key]) / seconds) : 0);
      setRates({ msgRate: rate('total_messages'), eventRate: rate('total_events') });
      previous.current = { time: now, stats: next.stats };
      setData(next);
      setOnline(true);
      if (!next.apps.some((a) => a.id === app)) setApp(next.apps[0] && next.apps[0].id);
    } catch {
      setOnline(false);
    }
  }, 2000, [app]);

  useInterval(async () => {
    try { setHistory((await api.get('/history')).points); } catch {}
  }, HISTORY_STEP_SECONDS * 1000);

  if (!data) {
    return html`<div class="empty" style="padding-top:30vh">${online ? 'Loading…' : 'Cannot reach the WSBro API.'}</div>`;
  }

  const view = VIEWS.find(([id]) => id === route) || VIEWS[0];
  const currentApp = data.apps.find((a) => a.id === app) || data.apps[0];
  const counts = { channels: currentApp && currentApp.channels, connections: currentApp && currentApp.connections };

  let page;
  switch (view[0]) {
    case 'channels': page = html`<${Channels} key=${app} app=${app} notify=${notify} />`; break;
    case 'connections': page = html`<${Connections} key=${app} app=${app} notify=${notify} />`; break;
    case 'console': page = html`<${Console} stream=${stream} />`; break;
    case 'apps': page = html`<${Apps} data=${data} app=${app} notify=${notify} />`; break;
    default: page = html`<${Overview} data=${data} history=${history} rates=${rates} app=${app} />`;
  }

  const themes = [['auto', 'monitor'], ['light', 'sun'], ['dark', 'moon']];

  return html`
    <div class="layout">
      <nav class="sidebar">
        <div class="brand"><img src="/dashboard/assets/logo.svg" alt="" /><div>WSBro<br /><small>${data.stats.version}</small></div></div>
        ${VIEWS.map(([id, label, icon]) => html`
          <button class=${'nav-item ' + (id === view[0] ? 'active' : '')} onClick=${() => { location.hash = '/' + id; }}>
            <${Icon} name=${icon} />${label}
            ${counts[id] !== undefined && html`<span class="count">${compact(counts[id])}</span>`}
          </button>`)}
        <div class="sidebar-foot">
          <div class="segmented" role="group" aria-label="Theme">
            ${themes.map(([mode, icon]) => html`
              <button class=${theme === mode ? 'active' : ''} onClick=${() => setTheme(mode)} title=${mode + ' theme'} aria-label=${mode + ' theme'} style="display:grid;place-items:center">
                <span style="width:14px;height:14px;display:block"><${Icon} name=${icon} /></span>
              </button>`)}
          </div>
          <span>${data.stats.adapter} adapter · node ${data.server.node_id.slice(0, 6)}</span>
        </div>
      </nav>
      <div class="content">
        <header class="topbar">
          <h1>${view[1]}</h1>
          <span class="spacer"></span>
          ${data.apps.length > 1 && html`
            <select class="select" value=${app} onChange=${(e) => setApp(e.target.value)} aria-label="Application">
              ${data.apps.map((a) => html`<option value=${a.id}>${a.id} · ${num(a.connections)} conn</option>`)}
            </select>`}
          <span class="pill"><span class=${'dot ' + (online ? 'live' : 'off')}></span>${online ? 'Live' : 'Offline'}</span>
        </header>
        <main class="page">${page}</main>
      </div>
    </div>
    <${Toast} toast=${toast} />`;
}

render(html`<${App} />`, document.getElementById('app'));
