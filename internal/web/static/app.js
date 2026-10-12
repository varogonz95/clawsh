// clawsh web UI: a dashboard of machines and sessions, plus a full-page terminal.
// Plain JS, no build step. It only talks to the clawsh web server that served it.
'use strict';

// Colors and the terminal font come from the stylesheet (app.css :root), so a
// visual redesign is a CSS change. The fallbacks match the original theme.
const rootStyle = getComputedStyle(document.documentElement);
function cssVar(name, fallback) { return rootStyle.getPropertyValue(name).trim() || fallback; }
const HOST_COLORS = cssVar('--host-colors', '#6a9cf2, #f0b43c, #52c27f, #e8875a, #b48cf0, #4fc1c9').split(',').map((c) => c.trim());
const AUTH_ERROR = 'access key rejected'; // hub.ErrMsgAuth
const CLOSE_NOT_FOUND = 4404; // web.closeNotFound
const CLOSE_UNREACHABLE = 4502; // web.closeUnreachable
const KEY_STORE = 'clawsh_key';

// takeKey moves the access key from the opened link (?k=…) into
// localStorage and out of the address bar. localStorage is scoped to this
// exact origin, port included, so pages on other local ports can't read it
// (a cookie would be sent to all of them).
function takeKey() {
  const k = new URLSearchParams(location.search).get('k');
  if (k) {
    try { localStorage.setItem(KEY_STORE, k); } catch (_) { /* storage blocked: keep it for this tab */ }
    history.replaceState(null, '', '/' + location.hash);
    return k;
  }
  try { return localStorage.getItem(KEY_STORE); } catch (_) { return null; }
}
const KEY = takeKey();

const ICONS = {
  plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>',
  back: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 18l-6-6 6-6"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>',
  folder: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
  eye: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg>',
};

const app = document.getElementById('app');
const state = {
  overview: null,       // {config_path, config_error?, hosts}, from the last SSE event
  live: 'connecting',   // connecting | live | reconnecting | signed-out
  view: null,           // the open session, see openSession
  dismissing: new Set(), // "host/id" of exited sessions being dismissed
};
let newDialog = null;

// ---------- helpers ----------

// h builds an element. Text children are text nodes, so names coming from
// agents can never inject markup.
function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'style') el.style.cssText = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

function icon(name, label) {
  const s = h('span', label ? { class: 'icon', role: 'img', 'aria-label': label } : { class: 'icon', 'aria-hidden': 'true' });
  s.innerHTML = ICONS[name]; // constant markup, never data
  return s;
}

function hostColor(name) {
  const hosts = state.overview ? state.overview.hosts : [];
  const i = hosts.findIndex((x) => x.name === name);
  return HOST_COLORS[Math.max(i, 0) % HOST_COLORS.length];
}

function tile(name, online) {
  return h('div', { class: online ? 'tile' : 'tile off', style: online ? `background:${hostColor(name)}` : null },
    name.charAt(0));
}

function hostAddr(url) {
  try { return new URL(url).hostname; } catch (_) { return url; }
}

function seconds(iso) { return Math.max(0, (Date.now() - Date.parse(iso)) / 1000); }

function shortAge(iso) {
  const s = seconds(iso);
  if (s < 60) return 'now';
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

function plural(n, word) { return `${n} ${word}${n === 1 ? '' : 's'}`; }

function longAge(iso) {
  const s = seconds(iso);
  if (s < 60) return 'just now';
  if (s < 3600) return `${plural(Math.floor(s / 60), 'minute')} ago`;
  if (s < 86400) return `${plural(Math.floor(s / 3600), 'hour')} ago`;
  return `${plural(Math.floor(s / 86400), 'day')} ago`;
}

function sessionLabel(s) { return s.name || s.id.slice(0, 8); }

function sessionPath(host, id) {
  return `/api/hosts/${encodeURIComponent(host)}/sessions/${encodeURIComponent(id)}`;
}

async function api(method, path, body) {
  const headers = { Authorization: `Bearer ${KEY}` };
  if (body) headers['Content-Type'] = 'application/json';
  const res = await fetch(path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error || msg; } catch (_) { /* not JSON */ }
    throw new Error(msg);
  }
  return res.status === 204 ? null : res.json();
}

function findSession(hostName, id) {
  const host = state.overview && state.overview.hosts.find((x) => x.name === hostName);
  return { host, session: host && host.sessions.find((x) => x.id === id) };
}

function toast(msg) {
  const t = h('div', { class: 'toast', role: 'alert' }, msg);
  document.body.append(t);
  setTimeout(() => t.remove(), 6000);
}

// keepFocus runs a re-render and gives focus back to the element carrying
// the same data-key, so keyboard users don't lose their place on updates.
function keepFocus(rerender) {
  const key = document.activeElement && document.activeElement.dataset ? document.activeElement.dataset.key : null;
  rerender();
  if (!key) return;
  const el = [...document.querySelectorAll('[data-key]')].find((x) => x.dataset.key === key);
  if (el) el.focus();
}

function go(hash) { location.hash = hash; }

// ---------- live data ----------

function connectEvents() {
  // EventSource and WebSocket can't set headers, so they carry the key in the query.
  const es = new EventSource(`/api/events?k=${encodeURIComponent(KEY)}`);
  es.addEventListener('overview', (e) => {
    state.overview = JSON.parse(e.data);
    state.live = 'live';
    render();
  });
  es.onerror = () => {
    // EventSource retries by itself unless the server refused us (CLOSED).
    // That happens when clawsh web was restarted and our key is stale.
    state.live = es.readyState === EventSource.CLOSED ? 'signed-out' : 'reconnecting';
    render();
  };
}

// ---------- routing ----------

function parseRoute() {
  let parts;
  try {
    parts = location.hash.replace(/^#\/?/, '').split('/').map(decodeURIComponent);
  } catch (_) {
    return { name: 'home' }; // a malformed hash, e.g. a stray "%"
  }
  if (parts[0] === 's' && parts.length === 3) return { name: 'session', host: parts[1], id: parts[2] };
  if (parts[0] === 'new') return { name: 'new', host: parts[1] || null };
  return { name: 'home' };
}

function onRoute() {
  const r = parseRoute();
  if (r.name === 'session') {
    if (!state.view || state.view.host !== r.host || state.view.id !== r.id) {
      closeSession();
      openSession(r.host, r.id);
    }
  } else {
    closeSession();
  }
  render();
}

function render() {
  if (state.live === 'signed-out') { renderSignedOut(); return; }
  const r = parseRoute();
  if (r.name === 'session') { renderSessionChrome(); return; }
  renderHome();
  syncNewDialog(r);
}

// ---------- dashboard ----------

function liveBadge() {
  const text = { connecting: 'Connecting…', live: 'Live', reconnecting: 'Reconnecting…' }[state.live];
  return h('span', { class: 'live' }, h('span', { class: state.live === 'live' ? 'dot ok' : 'dot' }), text);
}

function topbar() {
  const canCreate = state.overview && state.overview.hosts.some((x) => x.online);
  return h('header', { class: 'topbar' }, h('div', { class: 'topbar-in' },
    h('a', { class: 'brand', href: '#/', 'data-key': 'brand' }, h('span', { class: 'wordmark' }, 'claw', h('span', { class: 'sh' }, 'sh')), h('span', { class: 'mono small muted' }, 'hub')),
    h('div', { class: 'grow' }),
    liveBadge(),
    canCreate ? h('a', { class: 'btn primary', href: '#/new', 'data-key': 'new' }, icon('plus'), 'New session') : null));
}

function renderHome() {
  const ov = state.overview;
  let body;
  if (!ov) body = h('p', { class: 'muted' }, 'Looking for your machines…');
  else if (ov.hosts.length === 0) body = firstRun(ov);
  else body = [configError(ov), ...dashboard(ov)];
  keepFocus(() => app.replaceChildren(topbar(), h('main', { class: 'page' }, body)));
}

// configError explains why hosts.toml was not applied; the hosts shown are
// the last ones that parsed.
function configError(ov) {
  if (!ov.config_error) return null;
  return h('div', { class: 'banner config-error', role: 'alert' },
    `Your hosts file has an error: ${ov.config_error}. Fix it and save; this page updates by itself.`);
}

function dashboard(ov) {
  const online = ov.hosts.filter((x) => x.online);
  const running = online.reduce((n, x) => n + x.sessions.filter((s) => s.status !== 'exited' && s.status !== 'gone').length, 0);
  return [
    h('div', { class: 'page-head' },
      h('h1', {}, 'Your machines'),
      h('p', { class: 'muted' }, `${online.length} of ${ov.hosts.length} online · ${plural(running, 'session')} running`)),
    h('div', { class: 'cards' }, ov.hosts.map((x) => (x.online ? hostCard(x) : offlineCard(x)))),
  ];
}

function hostCard(host) {
  return h('section', { class: 'card' },
    h('div', { class: 'card-head' },
      tile(host.name, true),
      h('div', { class: 'grow' },
        h('h2', {}, host.name),
        h('div', { class: 'mono small muted' }, `${host.health.os} · ${hostAddr(host.url)}`)),
      h('span', { class: 'pill ok' }, 'Online')),
    host.sessions.map((s) => sessionRow(host.name, s)),
    h('div', { class: 'card-foot' },
      h('a', { class: 'link', href: `#/new/${encodeURIComponent(host.name)}`, 'data-key': `new:${host.name}` }, icon('plus'), `New session on ${host.name}`)));
}

function sessionRow(hostName, s) {
  const label = sessionLabel(s);
  if (s.status === 'exited') {
    return h('div', { class: 'row' },
      h('span', { class: 'dot hollow' }),
      h('span', { class: 'grow' },
        h('span', { class: 'row-title muted' }, label),
        h('span', { class: 'mono small muted' }, `Exited · code ${s.exit_code}`)),
      h('button', {
        class: 'btn ghost small', type: 'button', 'data-key': `dismiss:${hostName}/${s.id}`,
        disabled: state.dismissing.has(`${hostName}/${s.id}`), onclick: () => dismiss(hostName, s),
      }, 'Dismiss'));
  }
  return h('a', { class: 'row', href: `#/s/${encodeURIComponent(hostName)}/${encodeURIComponent(s.id)}`, 'data-key': `row:${hostName}/${s.id}` },
    h('span', { class: 'dot ok' }),
    h('span', { class: 'grow' },
      h('span', { class: 'row-title' }, label),
      h('span', { class: 'mono small muted' }, s.dir)),
    s.viewers > 0 ? h('span', { class: 'viewers small muted' }, icon('eye', 'viewers'), String(s.viewers)) : null,
    h('span', { class: 'mono small age' }, shortAge(s.created)));
}

async function dismiss(hostName, s) {
  const key = `${hostName}/${s.id}`;
  if (state.dismissing.has(key)) return;
  state.dismissing.add(key); // kept across re-renders until the request ends
  render();
  try {
    await api('DELETE', sessionPath(hostName, s.id));
    state.dismissing.delete(key); // the next overview drops the row
  } catch (e) {
    state.dismissing.delete(key);
    render();
    toast(`Couldn't dismiss ${sessionLabel(s)}: ${e.message}`);
  }
}

function offlineCard(host) {
  const body = host.error === AUTH_ERROR
    ? [h('p', {}, 'Access key rejected. Check hosts.toml.')]
    : [
      h('p', {}, "Can't reach this machine."),
      h('p', { class: 'mono small muted' }, host.error),
      h('p', { class: 'small muted' }, "Check that it's on and running ", h('code', {}, 'clawsh agent'),
        '. This card updates on its own when it comes back.'),
    ];
  return h('section', { class: 'card offline' },
    h('div', { class: 'card-head' },
      tile(host.name, false),
      h('div', { class: 'grow' }, h('h2', {}, host.name), h('div', { class: 'mono small muted' }, hostAddr(host.url))),
      h('span', { class: 'pill warn' }, 'Offline')),
    h('div', { class: 'card-body' }, body));
}

function firstRun(ov) {
  const step = (n, ...body) => h('li', { class: 'step' }, h('span', { class: 'step-n' }, String(n)), h('div', { class: 'grow' }, body));
  return h('div', { class: 'first-run' },
    configError(ov),
    h('h1', {}, 'No machines yet'),
    h('p', { class: 'muted' }, 'clawsh shows the machines listed in your hosts file. Add one in three steps:'),
    h('ol', { class: 'steps' },
      step(1, 'On that machine, run ', h('code', {}, 'clawsh agent')),
      step(2, 'Copy its access key with ', h('code', {}, 'clawsh token')),
      step(3, 'Add it to ', h('code', {}, ov.config_path), ' on this computer:',
        h('pre', { class: 'snippet' }, '[[host]]\nname  = "desk"\nurl   = "http://192.168.1.20:7420"\ntoken = "paste the access key here"'))),
    h('p', { class: 'small muted' }, 'This page updates by itself when you save the file. Adding machines from this page is coming later.'));
}

function renderSignedOut() {
  closeSession();
  if (newDialog) newDialog.close();
  app.replaceChildren(h('main', { class: 'page' }, h('div', { class: 'first-run' },
    h('h1', {}, KEY ? 'clawsh web was restarted' : 'Open clawsh from your terminal'),
    h('p', { class: 'muted' }, 'Open the link printed by clawsh web in your terminal.'))));
}

// ---------- new session ----------

function syncNewDialog(r) {
  if (r.name === 'new' && !newDialog && state.overview) openNewDialog(r.host);
  if (r.name !== 'new' && newDialog) newDialog.close();
}

function openNewDialog(preselect) {
  const hosts = state.overview.hosts;
  const chosen = hosts.find((x) => x.name === preselect && x.online) || hosts.find((x) => x.online);
  const err = h('p', { class: 'form-error', role: 'alert' });
  const dirHelp = h('span', { class: 'small muted' });
  const setHelp = (name) => { dirHelp.textContent = `A folder on ${name}. Leave it empty to use the home folder.`; };
  const submit = h('button', { class: 'btn primary', type: 'submit' }, 'Start session');

  const form = h('form', { class: 'dialog-body', onsubmit: (e) => { e.preventDefault(); submitNew(form, err, submit); } },
    h('div', {},
      h('h2', {}, 'Start a Claude session'),
      h('p', { class: 'muted' }, 'Claude starts on the machine you pick and keeps running after you close the browser.')),
    h('fieldset', {}, h('legend', {}, 'Machine'),
      h('div', { class: 'machines' }, hosts.map((x) => h('label', { class: x.online ? 'machine' : 'machine disabled' },
        h('input', { type: 'radio', name: 'host', value: x.name, checked: chosen && x.name === chosen.name, disabled: !x.online, onchange: () => setHelp(x.name) }),
        h('span', { class: 'machine-name' }, x.name),
        x.online ? null : h('span', { class: 'small' }, '· offline'))))),
    h('div', { class: 'field' },
      h('label', { for: 'ns-dir' }, 'Folder'),
      h('input', { id: 'ns-dir', name: 'dir', class: 'input mono', placeholder: '~', autocomplete: 'off' }),
      dirHelp),
    h('div', { class: 'field' },
      h('label', { for: 'ns-name' }, 'Name ', h('span', { class: 'muted' }, '(optional)')),
      h('input', { id: 'ns-name', name: 'name', class: 'input', placeholder: 'e.g. api-refactor', autocomplete: 'off' })),
    h('details', {}, h('summary', {}, 'Advanced'),
      h('div', { class: 'field' },
        h('label', { for: 'ns-args' }, 'Extra Claude arguments'),
        h('input', { id: 'ns-args', name: 'args', class: 'input mono', placeholder: '--resume', autocomplete: 'off' }))),
    err,
    h('div', { class: 'actions' },
      h('button', { class: 'btn ghost', type: 'button', onclick: () => newDialog.close() }, 'Cancel'),
      submit));

  newDialog = h('dialog', { class: 'dialog', 'aria-label': 'Start a Claude session' }, form);
  newDialog.addEventListener('close', () => {
    newDialog.remove();
    newDialog = null;
    if (parseRoute().name === 'new') go('#/');
  });
  document.body.append(newDialog);
  if (chosen) setHelp(chosen.name);
  else { submit.disabled = true; err.textContent = 'No machine is online right now.'; }
  newDialog.showModal();
}

// estimateTermSize guesses the session page's terminal size so claude starts
// close to it; the resize sent after attaching corrects the rest.
function estimateTermSize() {
  const width = Math.min(window.innerWidth, 1200) - 2 * 40 - 2 * 15;
  const height = window.innerHeight - 280;
  return { cols: Math.max(40, Math.floor(width / 8.4)), rows: Math.max(12, Math.floor(height / 17)) };
}

async function submitNew(form, err, submit) {
  const data = new FormData(form);
  const host = data.get('host');
  if (!host) { err.textContent = 'Pick a machine.'; return; }
  const text = (k) => String(data.get(k) || '').trim();
  const args = text('args').split(/\s+/).filter(Boolean);
  const size = estimateTermSize();
  submit.disabled = true;
  err.textContent = '';
  try {
    const s = await api('POST', `/api/hosts/${encodeURIComponent(host)}/sessions`, {
      name: text('name') || undefined,
      dir: text('dir') || undefined,
      args: args.length ? args : undefined,
      cols: size.cols,
      rows: size.rows,
    });
    go(`#/s/${encodeURIComponent(host)}/${encodeURIComponent(s.id)}`);
    if (newDialog) newDialog.close();
  } catch (e) {
    err.textContent = e.message;
    submit.disabled = false;
  }
}

// ---------- session ----------

function openSession(host, id) {
  const term = new Terminal({
    fontFamily: cssVar('--term-font', '"JetBrains Mono", ui-monospace, monospace'),
    fontSize: 14,
    cursorBlink: true,
    theme: {
      background: cssVar('--term-bg', '#0a0f14'),
      foreground: cssVar('--term-fg', '#d9e2ea'),
      cursor: cssVar('--term-fg', '#d9e2ea'),
      selectionBackground: cssVar('--term-selection', '#2b4a6e'),
    },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  const v = {
    host, id, term, fit,
    ws: null, timer: null, backoff: 1000,
    closed: false, exited: false, conn: 'connecting',
    liveSlot: h('span'),
    chromeEl: h('div', { class: 'session-chrome' }),
    bannerEl: h('div'),
    termEl: h('div', { class: 'term' }),
  };
  state.view = v;
  app.replaceChildren(
    h('header', { class: 'topbar' }, h('div', { class: 'topbar-in' },
      h('a', { class: 'back', href: '#/', 'data-key': 'back' }, icon('back'), 'All machines'),
      h('div', { class: 'grow' }),
      v.liveSlot)),
    h('div', { class: 'session' },
      v.chromeEl, v.bannerEl, v.termEl,
      h('p', { class: 'small muted' },
        `Leaving this page only disconnects you. Claude keeps working on ${host}, and you can come back any time.`)));
  renderSessionChrome();
  term.open(v.termEl);
  fit.fit();
  term.focus();
  const enc = new TextEncoder();
  term.onData((d) => send(v, enc.encode(d)));
  term.onBinary((d) => send(v, Uint8Array.from(d, (c) => c.charCodeAt(0))));
  term.onResize(({ cols, rows }) => sendResize(v, cols, rows));
  v.onWindowResize = () => fit.fit();
  window.addEventListener('resize', v.onWindowResize);
  connect(v);
}

function closeSession() {
  const v = state.view;
  if (!v) return;
  v.closed = true;
  state.view = null;
  clearTimeout(v.timer);
  window.removeEventListener('resize', v.onWindowResize);
  if (v.ws) v.ws.close();
  v.term.dispose();
}

function send(v, bytes) {
  if (v.ws && v.ws.readyState === WebSocket.OPEN) v.ws.send(bytes);
}

function sendResize(v, cols, rows) {
  if (v.ws && v.ws.readyState === WebSocket.OPEN) v.ws.send(JSON.stringify({ type: 'resize', cols, rows }));
}

function connect(v) {
  if (v.closed || v.exited) return;
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}${sessionPath(v.host, v.id)}/attach?k=${encodeURIComponent(KEY)}`);
  ws.binaryType = 'arraybuffer';
  v.ws = ws;
  let established = false;
  // The bridge accepts the browser socket before it dials the agent, so open
  // proves nothing. The first message from the agent does.
  ws.onopen = () => sendResize(v, v.term.cols, v.term.rows);
  ws.onmessage = (e) => {
    if (!established) {
      established = true;
      v.backoff = 1000;
      v.term.reset(); // the agent replays scrollback on every attach; don't show it twice
      v.conn = 'connected';
      setBanner(v, null);
      sendResize(v, v.term.cols, v.term.rows);
      renderSessionChrome();
    }
    if (typeof e.data !== 'string') { v.term.write(new Uint8Array(e.data)); return; }
    let msg;
    try { msg = JSON.parse(e.data); } catch (_) { return; }
    if (msg.type === 'exit') {
      v.exited = true;
      v.conn = 'ended';
      setBanner(v, h('div', { class: 'banner', role: 'status' }, `Claude exited (code ${msg.code}).`,
        h('a', { class: 'btn ghost small', href: '#/' }, 'Back to machines')));
      renderSessionChrome();
    }
  };
  ws.onclose = (e) => {
    if (v.closed || v.exited || state.view !== v) return;
    if (e.code === CLOSE_NOT_FOUND) {
      v.conn = 'ended';
      setBanner(v, h('div', { class: 'banner', role: 'status' }, 'This session no longer exists.',
        h('a', { class: 'btn ghost small', href: '#/' }, 'Back to machines')));
      renderSessionChrome();
      return;
    }
    if (e.code === CLOSE_UNREACHABLE && e.reason.startsWith(AUTH_ERROR)) {
      v.conn = 'ended';
      setBanner(v, h('div', { class: 'banner', role: 'status' }, 'Access key rejected. Check hosts.toml.',
        h('a', { class: 'btn ghost small', href: '#/' }, 'Back to machines')));
      renderSessionChrome();
      return;
    }
    v.conn = 'reconnecting';
    let text = 'Disconnected, reconnecting…';
    if (e.code === CLOSE_UNREACHABLE) text = e.reason ? `Can't reach ${v.host}: ${e.reason}. Retrying…` : `Can't reach ${v.host}. Retrying…`;
    setBanner(v, h('div', { class: 'banner', role: 'status' }, text));
    renderSessionChrome();
    v.timer = setTimeout(() => connect(v), v.backoff);
    v.backoff = Math.min(v.backoff * 2, 10000);
  };
}

function setBanner(v, el) {
  v.bannerEl.replaceChildren(...(el ? [el] : []));
  v.fit.fit();
}

function renderSessionChrome() {
  const v = state.view;
  if (!v) return;
  v.liveSlot.replaceChildren(liveBadge());
  const { host, session } = findSession(v.host, v.id);
  const label = session ? sessionLabel(session) : v.id.slice(0, 8);
  const connected = v.conn === 'connected';
  const connText = { connecting: 'Connecting…', connected: 'Connected', reconnecting: 'Reconnecting…', ended: 'Ended' }[v.conn];
  const viewers = session ? session.viewers : 0;
  keepFocus(() => v.chromeEl.replaceChildren(
    h('div', { class: 'session-head' },
      tile(v.host, !!(host && host.online)),
      h('div', { class: 'grow' }, h('div', { class: 'small muted' }, v.host), h('h1', {}, label)),
      v.exited || v.conn === 'ended' ? null
        : h('button', { class: 'btn danger', type: 'button', 'data-key': 'end', onclick: () => endSession(v, label) }, icon('x'), 'End session')),
    h('div', { class: 'chips' },
      session ? h('span', { class: 'chip' }, icon('folder'), h('span', { class: 'mono' }, session.dir)) : null,
      session ? h('span', { class: 'chip' }, `Started ${longAge(session.created)}`) : null,
      session && connected ? h('span', { class: 'chip' }, viewers <= 1 ? 'Only you are viewing' : `${viewers} people viewing`) : null,
      h('span', { class: connected ? 'chip ok' : 'chip' }, h('span', { class: connected ? 'dot ok' : 'dot' }), connText))));
}

function confirmDialog(title, text, action) {
  return new Promise((resolve) => {
    const dlg = h('dialog', { class: 'dialog small', 'aria-label': title },
      h('form', { method: 'dialog', class: 'dialog-body' },
        h('div', {}, h('h2', {}, title), h('p', { class: 'muted' }, text)),
        h('div', { class: 'actions' },
          h('button', { class: 'btn ghost', value: 'cancel' }, 'Cancel'),
          h('button', { class: 'btn danger-solid', value: 'ok' }, action))));
    dlg.addEventListener('close', () => { resolve(dlg.returnValue === 'ok'); dlg.remove(); });
    document.body.append(dlg);
    dlg.showModal();
  });
}

async function endSession(v, label) {
  const ok = await confirmDialog(`End ${label} on ${v.host}?`, 'Claude will stop. This cannot be undone.', 'End session');
  if (!ok) { v.term.focus(); return; }
  try {
    await api('DELETE', sessionPath(v.host, v.id));
    go('#/');
  } catch (e) {
    toast(`Couldn't end ${label}: ${e.message}`);
  }
}

// ---------- start ----------

window.addEventListener('hashchange', onRoute);
setInterval(() => { if (!state.view && !newDialog) render(); }, 30000); // keep ages fresh
// Load the terminal font first: xterm measures character size when it opens.
document.fonts.load('14px "JetBrains Mono"').finally(() => {
  if (!KEY) { state.live = 'signed-out'; render(); return; }
  connectEvents();
  onRoute();
});
