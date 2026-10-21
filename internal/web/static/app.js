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

// The version of clawsh web that served this page (index.html meta tag).
const metaVersion = document.querySelector('meta[name="clawsh-version"]');
const PAGE_VERSION = metaVersion ? metaVersion.content : '';
const RELOAD_STORE = 'clawsh.reloadedFor';

// semverCmp compares release versions by their numeric core; a pre-release
// sorts below its release. Returns null when either side isn't a version
// (dev builds), so callers never flag those.
function parseSemver(v) {
  if (/-\d+-g[0-9a-f]+|-dirty$/.test(String(v))) return null; // git describe: a build past a tag
  const m = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+.*)?$/.exec(String(v || ''));
  return m ? { core: [+m[1], +m[2], +m[3]], pre: m[4] || '' } : null;
}
function semverCmp(a, b) {
  const x = parseSemver(a), y = parseSemver(b);
  if (!x || !y) return null;
  for (let i = 0; i < 3; i++) if (x.core[i] !== y.core[i]) return x.core[i] < y.core[i] ? -1 : 1;
  if (x.pre === y.pre) return 0;
  if (!x.pre) return 1;
  if (!y.pre) return -1;
  const c = x.pre.localeCompare(y.pre, 'en', { numeric: true });
  return c < 0 ? -1 : c > 0 ? 1 : 0;
}

// hostOlder reports whether a host runs an older clawsh than this hub.
function hostOlder(host, hubVersion) {
  return !!(host.health && semverCmp(host.health.version, hubVersion) === -1);
}

// checkVersion reloads once into a new clawsh web version. If a reload for
// that version already happened and the page is still old, it asks instead of
// looping.
function checkVersion(ov) {
  if (!PAGE_VERSION || !ov.version || ov.version === PAGE_VERSION) return;
  let done = null;
  try { done = sessionStorage.getItem(RELOAD_STORE); } catch (_) { /* storage blocked */ }
  if (done === ov.version) { showUpdatedBanner(); return; }
  try { sessionStorage.setItem(RELOAD_STORE, ov.version); } catch (_) { /* reload anyway */ }
  location.reload(); // keeps the hash, so a session view comes back
}

function showUpdatedBanner() {
  if (document.getElementById('updated-banner')) return;
  const b = h('div', { id: 'updated-banner', class: 'banner updated', role: 'alert' },
    h('span', { class: 'grow' }, 'clawsh was updated. Reload to finish.'),
    h('button', { class: 'btn primary small', type: 'button', onclick: () => location.reload() }, 'Reload'));
  document.body.prepend(b);
}

const ICONS = {
  plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>',
  back: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 18l-6-6 6-6"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></svg>',
  folder: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
  copy: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V6a2 2 0 0 1 2-2h8"/></svg>',
  eye: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg>',
};

const app = document.getElementById('app');
const state = {
  overview: null,       // {config_path, config_error?, hosts}, from the last SSE event
  live: 'connecting',   // connecting | live | reconnecting | signed-out
  view: null,           // the open session, see openSession
  dismissing: new Set(), // "host/id" of exited sessions being dismissed
  lastSessions: new Map(), // host name -> sessions from its last online overview, so Duplicate still works once it drops offline
};
let newDialog = null;
let hostDialog = null;
let cfgHosts = null; // [{name,url,has_token}] from /api/config/hosts; null until loaded

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
    for (const x of state.overview.hosts) if (x.online) state.lastSessions.set(x.name, x.sessions); // offline hosts report none
    checkVersion(state.overview);
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
  if (parts[0] === 'hosts') return { name: 'hosts' };
  if (parts[0] === 'dup' && parts.length === 3) return { name: 'dup', host: parts[1], id: parts[2] };
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
  if (r.name === 'hosts') loadCfgHosts();
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
    h('a', { class: 'btn ghost small', href: '#/hosts', 'data-key': 'hosts' }, 'Machines'),
    canCreate ? h('a', { class: 'btn primary', href: '#/new', 'data-key': 'new' }, icon('plus'), 'New session') : null));
}

function renderHome() {
  const ov = state.overview;
  let body;
  if (parseRoute().name === 'hosts') body = hostsPage(ov);
  else if (!ov) body = h('p', { class: 'muted' }, 'Looking for your machines…');
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
        h('div', { class: 'mono small muted' }, `${host.health.os} · clawsh ${host.health.version} · ${hostAddr(host.url)}`)),
      olderPill(host),
      h('span', { class: 'pill ok' }, 'Online')),
    host.sessions.map((s) => sessionRow(host.name, s)),
    h('div', { class: 'card-foot' },
      h('a', { class: 'link', href: `#/new/${encodeURIComponent(host.name)}`, 'data-key': `new:${host.name}` }, icon('plus'), `New session on ${host.name}`)));
}

function dupButton(hostName, s) {
  return h('button', {
    class: 'btn ghost small icon-btn', type: 'button', 'aria-label': `Duplicate ${sessionLabel(s)}`, title: 'Duplicate',
    'data-key': `dup:${hostName}/${s.id}`, onclick: () => go(`#/dup/${encodeURIComponent(hostName)}/${encodeURIComponent(s.id)}`),
  }, icon('copy'));
}

function olderPill(host) {
  const ov = state.overview;
  if (!ov || !hostOlder(host, ov.version)) return null;
  return h('span', { class: 'pill warn', title: `run \`clawsh update\` on ${host.name}` }, 'older than this hub');
}

function sessionRow(hostName, s) {
  const label = sessionLabel(s);
  if (s.status === 'exited') {
    return h('div', { class: 'row' },
      h('span', { class: 'dot hollow' }),
      h('span', { class: 'grow' },
        h('span', { class: 'row-title muted' }, label),
        h('span', { class: 'mono small muted' }, `Exited · code ${s.exit_code}`)),
      dupButton(hostName, s),
      h('button', {
        class: 'btn ghost small', type: 'button', 'data-key': `dismiss:${hostName}/${s.id}`,
        disabled: state.dismissing.has(`${hostName}/${s.id}`), onclick: () => dismiss(hostName, s),
      }, 'Dismiss'));
  }
  // The button is a sibling of the link, never nested in it.
  return h('div', { class: 'row split' },
    h('a', { class: 'row-link', href: `#/s/${encodeURIComponent(hostName)}/${encodeURIComponent(s.id)}`, 'data-key': `row:${hostName}/${s.id}` },
      h('span', { class: 'dot ok' }),
      h('span', { class: 'grow' },
        h('span', { class: 'row-title' }, label),
        h('span', { class: 'mono small muted' }, s.dir)),
      s.viewers > 0 ? h('span', { class: 'viewers small muted' }, icon('eye', 'viewers'), String(s.viewers)) : null,
      h('span', { class: 'mono small age' }, shortAge(s.created))),
    dupButton(hostName, s));
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
    h('p', { class: 'muted' }, 'clawsh shows the machines listed in your hosts file. You can add one on the Machines page, or by hand in three steps:'),
    h('ol', { class: 'steps' },
      step(1, 'On that machine, run ', h('code', {}, 'clawsh agent')),
      step(2, 'Copy its access key with ', h('code', {}, 'clawsh token')),
      step(3, 'Add it to ', h('code', {}, ov.config_path), ' on this computer:',
        h('pre', { class: 'snippet' }, '[[host]]\nname  = "desk"\nurl   = "http://192.168.1.20:7420"\ntoken = "paste the access key here"'))),
    h('p', {}, h('a', { class: 'btn primary', href: '#/hosts' }, 'Add a machine here instead')),
    h('p', { class: 'small muted' }, 'This page also updates by itself when you save the file.'));
}

function renderSignedOut() {
  closeSession();
  if (newDialog) newDialog.close();
  app.replaceChildren(h('main', { class: 'page' }, h('div', { class: 'first-run' },
    h('h1', {}, KEY ? 'clawsh web was restarted' : 'Open clawsh from your terminal'),
    h('p', { class: 'muted' }, 'Open the link printed by clawsh web in your terminal.'))));
}

// ---------- machines (hosts.toml) ----------

async function loadCfgHosts() {
  try {
    cfgHosts = await api('GET', '/api/config/hosts');
  } catch (e) {
    toast(`Couldn't load machines: ${e.message}`);
    cfgHosts = cfgHosts || [];
  }
  if (parseRoute().name === 'hosts' && !hostDialog) render();
}

function hostsPage(ov) {
  const live = (name) => ov && ov.hosts.find((x) => x.name === name);
  const rows = (cfgHosts || []).map((x) => {
    const o = live(x.name);
    const status = !o ? null : o.online ? h('span', { class: 'pill' }, 'Online')
      : h('span', { class: 'pill warn' }, o.error === AUTH_ERROR ? 'Key rejected' : 'Offline');
    return h('li', { class: 'host-row' },
      tile(x.name, !!(o && o.online)),
      h('div', { class: 'grow' }, h('strong', {}, x.name),
        h('div', { class: 'mono small muted' }, o && o.online ? `${o.health.os} · clawsh ${o.health.version} · ${hostAddr(x.url)}` : hostAddr(x.url)),
        o && o.online && ov && hostOlder(o, ov.version) ? h('div', { class: 'small warn-text' }, 'older than this hub: run ', h('code', {}, 'clawsh update'), ` on ${x.name}`) : null),
      o && o.online ? olderPill(o) : null,
      status,
      h('button', { class: 'btn ghost small', type: 'button', onclick: () => openHostDialog(x) }, 'Edit'),
      h('button', { class: 'btn danger small', type: 'button', onclick: () => removeHost(x) }, 'Remove'));
  });
  return [
    configError(ov),
    h('div', { class: 'first-run' },
      h('h1', {}, 'Machines'),
      h('p', { class: 'muted' }, 'The machines in ', h('code', {}, ov ? ov.config_path : 'hosts.toml'),
        '. Access keys are saved but never shown again. Saving here rewrites that file and drops any comments in it; the previous version is kept as ',
        h('code', {}, 'hosts.toml.bak'), '.'),
      cfgHosts === null ? h('p', { class: 'muted' }, 'Loading…')
        : rows.length ? h('ul', { class: 'host-list' }, rows) : h('p', {}, 'No machines yet.'),
      h('p', {}, h('button', { class: 'btn primary', type: 'button', onclick: () => openHostDialog(null) }, icon('plus'), 'Add machine'))),
  ];
}

async function removeHost(x) {
  const ok = await confirmDialog(`Remove ${x.name}?`, 'It is removed from hosts.toml. Its sessions keep running on that machine.', 'Remove');
  if (!ok) return;
  try {
    await api('DELETE', `/api/config/hosts/${encodeURIComponent(x.name)}`);
    await loadCfgHosts();
  } catch (e) {
    toast(`Couldn't remove ${x.name}: ${e.message}`);
  }
}

// openHostDialog adds a machine (existing null) or edits one. The access key
// field is write-only: empty keeps the saved key.
function openHostDialog(existing) {
  const err = h('p', { class: 'form-error', role: 'alert' });
  const result = h('p', { class: 'small', role: 'status' });
  const save = h('button', { class: 'btn primary', type: 'submit' }, existing ? 'Save' : 'Add machine');
  const test = h('button', { class: 'btn ghost', type: 'button' }, 'Test connection');
  const read = () => {
    const f = new FormData(form);
    const t = (k) => String(f.get(k) || '').trim();
    return { name: t('name'), url: t('url'), token: String(f.get('token') || '') };
  };
  const form = h('form', { class: 'dialog-body', onsubmit: async (e) => {
    e.preventDefault();
    save.disabled = true;
    err.textContent = '';
    try {
      const body = read();
      if (existing) await api('PUT', `/api/config/hosts/${encodeURIComponent(existing.name)}`, body);
      else await api('POST', '/api/config/hosts', body);
      hostDialog.close();
      await loadCfgHosts();
    } catch (ex) {
      err.textContent = ex.message;
      save.disabled = false;
    }
  } },
    h('h2', {}, existing ? `Edit ${existing.name}` : 'Add a machine'),
    h('div', { class: 'field' }, h('label', { for: 'hd-name' }, 'Name'),
      h('input', { id: 'hd-name', name: 'name', class: 'input', value: existing ? existing.name : '', placeholder: 'desk', autocomplete: 'off', required: true })),
    h('div', { class: 'field' }, h('label', { for: 'hd-url' }, 'Address'),
      h('input', { id: 'hd-url', name: 'url', class: 'input mono', value: existing ? existing.url : '', placeholder: 'http://192.168.1.20:7420', autocomplete: 'off', required: true })),
    h('div', { class: 'field' }, h('label', { for: 'hd-token' }, 'Access key'),
      h('input', { id: 'hd-token', name: 'token', class: 'input mono', type: 'password', autocomplete: 'off',
        placeholder: existing ? 'Leave empty to keep the saved key' : 'From clawsh token on that machine', required: !existing })),
    result, err,
    h('div', { class: 'actions' },
      h('button', { class: 'btn ghost', type: 'button', onclick: () => hostDialog.close() }, 'Cancel'),
      test, save));
  test.addEventListener('click', async () => {
    const b = read();
    result.textContent = 'Testing…';
    err.textContent = '';
    try {
      const r = await api('POST', '/api/config/hosts/test', existing ? { ...b, name: existing.name } : b);
      result.textContent = r.ok ? 'Connected.' : ({ unreachable: "Can't reach this address.", unauthorized: 'The machine rejected this access key.' }[r.error] || r.error);
    } catch (ex) {
      result.textContent = '';
      err.textContent = ex.message;
    }
  });
  hostDialog = h('dialog', { class: 'dialog', 'aria-label': existing ? 'Edit machine' : 'Add a machine' }, form);
  hostDialog.addEventListener('close', () => { hostDialog.remove(); hostDialog = null; render(); });
  document.body.append(hostDialog);
  hostDialog.showModal();
}

// ---------- new session ----------

function syncNewDialog(r) {
  const open = r.name === 'new' || r.name === 'dup';
  if (open && !newDialog && state.overview) {
    if (r.name === 'new') openNewDialog({ host: r.host });
    else {
      const host = state.overview.hosts.find((x) => x.name === r.host);
      const src = host ? (host.online ? host.sessions : state.lastSessions.get(r.host) || []).find((x) => x.id === r.id) : null;
      if (!src) { toast('That session is no longer listed'); go('#/'); return; }
      openNewDialog({ host: r.host, dir: src.dir, name: src.name, args: src.args, title: `Duplicate ${sessionLabel(src)}` });
    }
  }
  if (!open && newDialog) newDialog.close();
}

// joinArgs renders args as one editable line; splitArgs is its inverse.
function joinArgs(args) {
  return (args || []).map((a) => (/[\s"\\]/.test(a) || a === '' ? `"${a.replace(/["\\]/g, '\\$&')}"` : a)).join(' ');
}

function splitArgs(text) {
  const out = [];
  let cur = '', has = false, quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '\\' && i + 1 < text.length && (text[i + 1] === '"' || text[i + 1] === '\\')) cur += text[++i];
      else if (c === '"') quoted = false;
      else cur += c;
    } else if (c === '"') { quoted = true; has = true; }
    else if (/\s/.test(c)) { if (has || cur) out.push(cur); cur = ''; has = false; }
    else { cur += c; has = true; }
  }
  if (quoted) throw new Error('Unclosed quote in the arguments.');
  if (has || cur) out.push(cur);
  return out;
}

// nextName proposes a name for a copy of a session called name on hostName:
// the base name if free, else base (2), base (3), ...
function nextName(name, hostName) {
  if (!name) return '';
  const base = name.replace(/ \(\d+\)$/, '');
  const host = state.overview && state.overview.hosts.find((x) => x.name === hostName);
  const taken = new Set((host ? host.sessions : []).filter((x) => x.status !== 'exited').map((x) => x.name));
  if (!taken.has(base)) return base;
  for (let n = 2; ; n++) if (!taken.has(`${base} (${n})`)) return `${base} (${n})`;
}

function openNewDialog(opts) {
  const { host: preselect, dir = '', name = '', args = [], title } = opts;
  const hosts = state.overview.hosts;
  const chosen = hosts.find((x) => x.name === preselect && x.online) || hosts.find((x) => x.online);
  const offlineNote = preselect && title && chosen && chosen.name !== preselect ? `${preselect} is offline; pick another machine.` : '';
  const err = h('p', { class: 'form-error', role: 'alert' });
  const dirHelp = h('span', { class: 'small muted' });
  const setHelp = (n) => { dirHelp.textContent = `A folder on ${n}. Leave it empty to use the home folder.`; };
  const submit = h('button', { class: 'btn primary', type: 'submit' }, 'Start session');
  const nameInput = h('input', { id: 'ns-name', name: 'name', class: 'input', placeholder: 'e.g. api-refactor', autocomplete: 'off', value: title && chosen ? nextName(name, chosen.name) : '' });
  let nameEdited = false;
  nameInput.addEventListener('input', () => { nameEdited = true; });
  const onHost = (n) => {
    pick(n);
    if (title && !nameEdited) nameInput.value = nextName(name, n);
  };
  const recent = h('div', { class: 'recent', hidden: true });
  let recentSeq = 0;
  // loadRecent shows the machine's recent folders; any failure just hides the list.
  const loadRecent = async (name) => {
    const seq = ++recentSeq;
    recent.hidden = true;
    let dirs = [];
    try {
      dirs = (await api('GET', `/api/hosts/${encodeURIComponent(name)}/dirs/recent`)).dirs || [];
    } catch (e) { /* the folder field still works */ }
    if (seq !== recentSeq || !newDialog) return;
    renderRecent(recent, dirs, document.getElementById('ns-dir'));
  };
  const pick = (n) => { setHelp(n); loadRecent(n); };

  const form = h('form', { class: 'dialog-body', onsubmit: (e) => { e.preventDefault(); submitNew(form, err, submit); } },
    h('div', {},
      h('h2', {}, title || 'Start a Claude session'),
      h('p', { class: 'muted' }, 'Claude starts on the machine you pick and keeps running after you close the browser.')),
    h('fieldset', {}, h('legend', {}, 'Machine'),
      h('div', { class: 'machines' }, hosts.map((x) => h('label', { class: x.online ? 'machine' : 'machine disabled' },
        h('input', { type: 'radio', name: 'host', value: x.name, checked: chosen && x.name === chosen.name, disabled: !x.online, onchange: () => onHost(x.name) }),
        h('span', { class: 'machine-name' }, x.name),
        x.online ? null : h('span', { class: 'small' }, '· offline')))),
      offlineNote ? h('p', { class: 'small muted' }, offlineNote) : null),
    h('div', { class: 'field' },
      h('label', { for: 'ns-dir' }, 'Folder'),
      h('input', { id: 'ns-dir', name: 'dir', class: 'input mono', placeholder: '~', autocomplete: 'off', value: dir }),
      dirHelp, recent),
    h('div', { class: 'field' },
      h('label', { for: 'ns-name' }, 'Name ', h('span', { class: 'muted' }, '(optional)')),
      nameInput),
    h('details', { open: args.length > 0 }, h('summary', {}, 'Advanced'),
      h('div', { class: 'field' },
        h('label', { for: 'ns-args' }, 'Extra Claude arguments'),
        h('input', { id: 'ns-args', name: 'args', class: 'input mono', placeholder: '--resume', autocomplete: 'off', value: joinArgs(args) }))),
    err,
    h('div', { class: 'actions' },
      h('button', { class: 'btn ghost', type: 'button', onclick: () => newDialog.close() }, 'Cancel'),
      submit));

  newDialog = h('dialog', { class: 'dialog', 'aria-label': title || 'Start a Claude session' }, form);
  newDialog.addEventListener('close', () => {
    newDialog.remove();
    newDialog = null;
    const n = parseRoute().name;
    if (n === 'new' || n === 'dup') go('#/');
  });
  document.body.append(newDialog);
  if (chosen) pick(chosen.name);
  else { submit.disabled = true; err.textContent = 'No machine is online right now.'; }
  newDialog.showModal();
}

const RECENT_SHOWN = 3;

// renderRecent fills the recent-folders list; clicking an entry fills the
// folder field without submitting.
function renderRecent(box, dirs, input) {
  if (!dirs.length) { box.hidden = true; box.replaceChildren(); return; }
  let all = false;
  const draw = () => {
    const shown = all ? dirs : dirs.slice(0, RECENT_SHOWN);
    const kids = [h('span', { class: 'small muted' }, 'Recent'),
      ...shown.map((d) => h('button', {
        type: 'button', class: 'recent-item mono', title: d,
        onclick: () => { input.value = d; input.focus(); },
      }, h('span', {}, d)))];
    if (dirs.length > RECENT_SHOWN) {
      kids.push(h('button', {
        type: 'button', class: 'recent-toggle small',
        onclick: () => { all = !all; draw(); },
      }, all ? 'Show less' : `Show all (${dirs.length})`));
    }
    box.replaceChildren(...kids);
  };
  draw();
  box.hidden = false;
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
  let args;
  try { args = splitArgs(text('args')); } catch (e) { err.textContent = e.message; return; }
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
  attachScrollThumb(term, v.termEl);
  term.focus();
  const enc = new TextEncoder();
  // xterm sends \r for Shift+Enter too; Claude Code reads ESC+CR as "newline".
  term.attachCustomKeyEventHandler((ev) => {
    if (!clawshKeys.isShiftEnter(ev)) return true;
    // Send on keydown only, but swallow keypress/keyup too: xterm sends \r
    // from keypress when keydown was not marked handled.
    if (clawshKeys.shiftEnterNewline(ev)) send(v, enc.encode('\x1b\r'));
    return false;
  });
  term.onData((d) => send(v, enc.encode(d)));
  term.onBinary((d) => send(v, Uint8Array.from(d, (c) => c.charCodeAt(0))));
  term.onResize(({ cols, rows }) => sendResize(v, cols, rows));
  v.onWindowResize = () => fit.fit();
  window.addEventListener('resize', v.onWindowResize);
  connect(v);
}

// Overlay scrollbar: round, translucent, visible only while the buffer scrolls (or while hovered/dragged).
function attachScrollThumb(term, host) {
  const thumb = h('div', { class: 'scrollthumb' });
  host.appendChild(thumb);
  let hideTimer = null;
  let dragging = false;
  const geometry = () => {
    const buf = term.buffer.active;
    const total = buf.baseY + term.rows;
    const track = host.clientHeight - 16;
    const size = Math.max(28, track * term.rows / total);
    return { buf, total, track, size, range: track - size };
  };
  const place = () => {
    const { buf, size, range } = geometry();
    const max = buf.baseY;
    thumb.style.height = size + 'px';
    thumb.style.top = 8 + (max ? range * buf.viewportY / max : range) + 'px';
    thumb.style.display = max ? '' : 'none';
  };
  const poke = () => {
    place();
    thumb.classList.add('on');
    clearTimeout(hideTimer);
    hideTimer = setTimeout(() => { if (!dragging && !thumb.matches(':hover')) thumb.classList.remove('on'); }, 1000);
  };
  term.onScroll(poke);
  // The viewport's own scroll event also covers wheel/touch scrolling, which term.onScroll misses.
  host.querySelector('.xterm-viewport').addEventListener('scroll', poke, { passive: true });
  term.onResize(place);
  thumb.addEventListener('pointerenter', () => { clearTimeout(hideTimer); thumb.classList.add('on'); });
  thumb.addEventListener('pointerleave', () => { if (!dragging) poke(); });
  thumb.addEventListener('pointerdown', (e) => {
    const { buf, range } = geometry();
    const startY = e.clientY, startLine = buf.viewportY;
    dragging = true;
    thumb.classList.add('drag');
    thumb.setPointerCapture(e.pointerId);
    const move = (m) => {
      if (range > 0) term.scrollToLine(Math.round(startLine + (m.clientY - startY) * buf.baseY / range));
    };
    const up = () => {
      dragging = false;
      thumb.classList.remove('drag');
      thumb.removeEventListener('pointermove', move);
      thumb.removeEventListener('pointerup', up);
      thumb.removeEventListener('pointercancel', up);
      poke();
    };
    thumb.addEventListener('pointermove', move);
    thumb.addEventListener('pointerup', up);
    thumb.addEventListener('pointercancel', up);
    e.preventDefault();
  });
  place();
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
