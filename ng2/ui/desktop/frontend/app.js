// Claude Launcher — interface native (Wails) ou web (mode --web).
// Sans framework : l'état vient du Go (événements « state », « cards »,
// « term »), l'interface ne fait que l'afficher et renvoyer les actions
// (Dispatch côté Go). Terminaux : xterm.js, un par pane tmux, placés à la
// géométrie de tmux (mise en page des teams de core).
'use strict';

// ── Transport : Wails (window.go) ou HTTP + SSE ────────────────────────────
const SESSION_MSG = 'session expirée — rouvrir le lien d\'accès';
const DENIED_MSG = 'accès refusé : adresse ou origine non admise (voir Réglages › Interface web › Adresse publique)';
let sessionIsLost = false; // vrai aussi après un refus d'hôte/origine : on ne réessaie plus
function stopBanner(msg) {
  if (sessionIsLost) return;
  sessionIsLost = true;
  const b = document.createElement('div');
  b.className = 'lanbanner sessionlost'; b.id = 'sessionlost'; b.textContent = '⚠ ' + msg;
  document.body.insertBefore(b, document.body.firstChild);
}
const sessionLost = () => stopBanner(SESSION_MSG);
const accessDenied = () => stopBanner(DENIED_MSG);
// défense en profondeur : retirer le jeton d'amorçage (?t=) de la barre d'adresse et de l'historique
try { const u = new URL(location.href); if (u.searchParams.has('t')) { u.searchParams.delete('t'); history.replaceState(null, '', u.pathname + (u.search || '') + u.hash); } } catch (e) { /* sans effet */ }
// Flux d'évènements (SSE) et reconnexion (#171, #198). Le flux n'envoie aucun état à l'ouverture : à chaque
// (ré)ouverture après coupure, `init` est rejoué D'ABORD (état, version et démarrage du serveur) ; tant qu'il échoue
// (pod qui démarre : 502/503 de l'ingress), le flux n'est pas rouvert et l'essai est répété (1,5 s doublé jusqu'à
// 15 s). Bandeau « connexion perdue » pendant la coupure. 401/403 (session perdue) : on arrête (#171).
function startFlow(env) {
  let delay = 1500, down = false;
  const lose = () => { if (!down) { down = true; env.banner(true); } };
  // preuve que le serveur répond (init réussi, flux ouvert, message reçu) : bandeau retiré, quel que soit le chemin
  const alive = () => { delay = 1500; if (down) { down = false; env.banner(false); } };
  const retry = () => { if (env.lost()) return; lose(); const w = delay; delay = Math.min(delay * 2, 15000); env.setTimeout(reconnect, w); };
  // veille : le serveur envoie un évènement « ping » toutes les 15 s ; rien pendant 40 s (flux à moitié ouvert, sans
  // onerror) : flux déclaré mort, fermé, bandeau et reprise par init.
  const open = () => {
    const es = env.newES('/api/events');
    let wd = null, dead = false;
    const arm = () => { env.unwatch(wd); if (!dead) wd = env.watch(() => { if (dead) return; dead = true; es.close(); console.warn('[flux] aucun évènement depuis 40 s : flux fermé'); retry(); }, 40000); };
    arm();
    es.onopen = () => { alive(); arm(); };
    es.onmessage = (m) => { alive(); arm(); try { const e = JSON.parse(m.data); env.cb(e.name, e.data); } catch (err) { console.error(err); } };
    es.onerror = () => { dead = true; env.unwatch(wd); es.close(); retry(); };
  };
  const reconnect = async () => {
    let r;
    try { r = await env.race(env.init(), 8000); } catch (err) { if (env.lost()) return; console.warn('[flux] init échoué, nouvel essai :', err && err.message); retry(); return; }
    alive();
    try { env.applyInit(r); } catch (err) { console.error('[flux] erreur d\'applyInit :', err); }
    open();
  };
  open();
}
function connBanner(on) {
  let b = document.getElementById('connbanner');
  if (!on) { if (b) b.remove(); return; }
  if (b) return;
  b = document.createElement('div'); b.id = 'connbanner'; b.className = 'lanbanner'; b.textContent = '⚠ connexion au serveur perdue — nouvel essai…';
  const main = document.getElementById('main'), tabs = document.getElementById('tabs');
  if (main && tabs && tabs.parentNode === main) main.insertBefore(b, tabs); else document.body.insertBefore(b, document.body.firstChild);
}
const transport = (() => {
  const wails = () => window.go && window.go.desktop && window.go.desktop.Bridge;
  return {
    async call(method, args) {
      if (wails()) {
        const r = await window.go.desktop.Bridge.Call(method, JSON.stringify(args || {}));
        return r ? JSON.parse(r) : null;
      }
      const res = await fetch('/api/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ method, args: args || {} }) });
      if (res.status === 401 || res.status === 403) { // session perdue, ou accès refusé (hôte/origine) : en-tête X-Launcher-Refusal du Go (#171)
        const why = res.headers.get('X-Launcher-Refusal');
        if (why === 'host' || why === 'origin') { accessDenied(); throw new Error(DENIED_MSG); }
        sessionLost(); throw new Error(SESSION_MSG); // `session`, ou refus sans en-tête
      }
      const j = await res.json();
      if (j.error) throw new Error(j.error);
      return j.result;
    },
    listen(cb) {
      if (window.runtime && window.runtime.EventsOn) {
        window.runtime.EventsOn('ev', (e) => cb(e.name, e.data));
        return;
      }
      startFlow({ watch: (f, ms) => setTimeout(f, ms), unwatch: (h) => clearTimeout(h), newES: (u) => new EventSource(u), race: (p, ms) => Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error('délai dépassé')), ms))]), init: () => transport.call('init'), applyInit, cb, setTimeout, banner: connBanner, lost: () => sessionIsLost });
    },
  };
})();
// Client web ou fenêtre native (#163) : la fenêtre native est celle qui expose Wails.
const isWebClient = () => !(window.go && window.go.desktop && window.go.desktop.Bridge);
// Client web d'une fenêtre native qui sert l'interface web (webUI actif) : quit, setTheme, webUIOpen et le
// presse-papiers du poste lui sont refusés par le Go (« desktop --web » seul : webUI inactif, tout y reste permis).
const webOfNative = () => isWebClient() && !!(S.snap && S.snap.webUI && S.snap.webUI.state === 'on');
// Plusieurs clients possibles sur les mêmes terminaux : seul celui qui a le clavier (fenêtre au premier plan)
// fixe la taille ; un client seul (webUI inactif) la fixe toujours.
const sharedTerms = () => !!(S.snap && S.snap.webUI && S.snap.webUI.state === 'on');
const hasKeyboard = () => !sharedTerms() || document.hasFocus();
const call = (m, a) => transport.call(m, a).catch((e) => { flash('⚠ ' + e.message); throw e; });

// ── Utilitaires ────────────────────────────────────────────────────────────
const $ = (id) => document.getElementById(id);
const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text != null) e.textContent = text; return e; };
const tone = (t) => (t ? 't-' + (t.color || 'dim') + (t.bold ? ' b' : '') : '');
const store = {
  get(k, d) { try { const v = localStorage.getItem('cl.' + k); return v == null ? d : JSON.parse(v); } catch (e) { return d; } },
  set(k, v) { try { localStorage.setItem('cl.' + k, JSON.stringify(v)); } catch (e) { /* stockage indisponible */ } },
};
const MONO = getComputedStyle(document.documentElement).getPropertyValue('--mono').trim() || 'monospace';
// Taille des caractères des terminaux (#140) : préférence termFontSize du poste,
// changée par Ctrl+molette (setFontSize).
const FONT_DEFAULT = 13, MIN_FONT = 8, MAX_FONT = 28;
let FONT_SIZE = FONT_DEFAULT;
// Taille des caractères de l'arbre (zone du navigateur) et des cartes (#150) : préférences
// treeFontSize / cardsFontSize, mêmes bornes que les terminaux ; la page suit --tfs / --cfs.
const TREE_DEFAULT = 13, CARDS_DEFAULT = 12;
let TREE_SIZE = TREE_DEFAULT, CARDS_SIZE = CARDS_DEFAULT;
const clampFont = (n, d) => { n = Math.round(Number(n)); return Number.isFinite(n) ? Math.min(MAX_FONT, Math.max(MIN_FONT, n)) : (d || FONT_DEFAULT); };

// ── État ───────────────────────────────────────────────────────────────────
const S = {
  snap: null, cards: null, help: null, tools: null,
  filter: '', cursor: null,      // chemin du projet sous le curseur ('new' : nouveau projet)
  view: 'leaders',               // onglet affiché : 'leaders' ou chemin d'un projet
  views: {},                     // chemin -> vue de projet
  quit: 0,                       // 1 : question l/t ; 2 : confirmer « tout fermer »
  lastClick: { key: null, at: 0 },
  prefix: false,                 // Ctrl+B tapé dans un terminal
  focusNav: true,
};

function flash(m) { const e = $('msg'); e.textContent = m; }

// Message bref dans le pied de volet : s'efface seul s'il n'a pas été remplacé entre-temps.
let flashTimer = null;
function flashBrief(m, ms) {
  flash(m);
  clearTimeout(flashTimer);
  flashTimer = setTimeout(() => { if ($('msg').textContent === m) flash(''); }, ms || 2500);
}

// ── Presse-papiers et collage dans les terminaux (#155, LOT-4) ──────────────
// Fenêtre native : clipboardWrite / clipboardRead du Go (presse-papiers du poste, sans invite
// WebView2). « unavailable » (--web) : navigator.clipboard, en contexte sécurisé seulement.
async function clipCall(method, args) {
  try { return (await transport.call(method, args)) || { ok: false, unavailable: true }; }
  catch (e) { return { ok: false, unavailable: true, msg: e.message }; }
}
async function clipWrite(text) {
  const r = await clipCall('clipboardWrite', { text });
  if (r.ok || !r.unavailable) return r;
  if (!window.isSecureContext || !navigator.clipboard) return { ok: false, msg: r.msg || 'presse-papiers indisponible : page hors contexte sécurisé (utiliser http://localhost)' };
  try { await navigator.clipboard.writeText(text); return { ok: true }; } catch (e) { return { ok: false, msg: 'copie refusée par le navigateur : ' + e.message }; }
}
async function clipRead() {
  const r = await clipCall('clipboardRead', {});
  if (r.ok || !r.unavailable) return r;
  if (!window.isSecureContext || !navigator.clipboard) return { ok: false, msg: r.msg || 'presse-papiers indisponible : page hors contexte sécurisé (utiliser http://localhost)' };
  try { return { ok: true, text: await navigator.clipboard.readText() }; } catch (e) { return { ok: false, msg: 'lecture refusée par le navigateur : ' + e.message }; }
}
// Résultat de termInput : vide si la saisie est partie ; sinon {ok:false, msg}.
const termInLast = {}; // motif -> date du dernier message (au plus un par seconde et par motif, comme côté Go)
const isMouseInput = (a) => a.binary || /^\x1b\[(<|M)/.test(a.data || ''); // souris X10 / SGR : jamais de message
const termIn = (a) => call('termInput', a).then((r) => {
  if (r && r.ok === false && !isMouseInput(a)) { // reason : 'readonly' (un autre launcher a la main) | 'toobig' (collage > 4 Mo)
    const k = r.reason || 'refus', now = Date.now();
    if (!termInLast[k] || now - termInLast[k] >= 1000) {
      termInLast[k] = now;
      flashBrief('⚠ ' + (r.msg || (r.reason === 'readonly' ? 'lecture seule : saisie refusée' : 'saisie refusée')), 4000);
    }
  }
  return r;
});

// Copie / collage d'un terminal. clip = { term, readOnly(): bool } ; la saisie collée passe par term.paste
// (collage entre crochets si le programme l'a demandé), donc par le onData du terminal.
async function clipCopy(term) {
  if (!term.hasSelection()) return;
  const r = await clipWrite(term.getSelection());
  if (r.ok) flashBrief('Copié'); else flashBrief('⚠ Copie impossible : ' + (r.msg || 'refusée'), 4000);
}
async function clipPaste(clip) {
  if (clip.readOnly()) { flashBrief('⚠ Collage refusé : projet en lecture seule (un autre launcher a la main)', 4000); return; }
  const r = await clipRead();
  if (!r.ok) { flashBrief('⚠ Collage impossible : ' + (r.msg || 'refusé'), 4000); return; }
  if (!r.text) { flashBrief('Presse-papiers vide (pas de texte)'); return; }
  clip.term.paste(r.text);
}
function attachClip(clip) {
  const term = clip.term, el0 = term.element;
  if (!el0) return;
  // copie à la fin de la sélection (bouton relâché, même hors du terminal)
  el0.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return;
    const up = () => { document.removeEventListener('mouseup', up, true); setTimeout(() => clipCopy(term), 0); };
    document.addEventListener('mouseup', up, true);
  });
  // clic droit : coller ; si le programme suit la souris, Maj+clic droit
  el0.addEventListener('contextmenu', (e) => {
    let tracking = false;
    try { tracking = term.modes.mouseTrackingMode !== 'none'; } catch (err) { /* API absente : collage */ }
    if (tracking && !e.shiftKey) return;
    e.preventDefault();
    clipPaste(clip);
  });
}

// ── Mesure des cellules du terminal ────────────────────────────────────────
let cell = null;
function measureCell() {
  const s = el('span');
  s.style.cssText = `position:absolute;visibility:hidden;white-space:pre;font-family:${MONO};font-size:${FONT_SIZE}px;line-height:normal`;
  s.textContent = 'W'.repeat(200);
  document.body.appendChild(s);
  const r = s.getBoundingClientRect();
  document.body.removeChild(s);
  return { w: r.width / 200, h: Math.ceil(r.height) };
}
function refineCell(term) {
  try {
    const d = term._core._renderService.dimensions.css.cell;
    if (d && d.width > 0 && d.height > 0) cell = { w: d.width, h: d.height };
  } catch (e) { /* API interne absente : mesure de repli */ }
}

function newTerm(opts) {
  const t = new Terminal(Object.assign({
    fontFamily: MONO, fontSize: FONT_SIZE, lineHeight: 1, scrollback: 3000, allowProposedApi: true,
    cursorBlink: true, macOptionIsMeta: true, drawBoldTextInBrightColors: false,
    theme: { background: '#0A0C10', foreground: '#E6E8EC', cursor: '#F0A868', selectionBackground: '#3A4458' },
  }, opts || {}));
  try { t.loadAddon(new Unicode11Addon.Unicode11Addon()); t.unicode.activeVersion = '11'; } catch (e) { /* largeurs Unicode 6 */ }
  return t;
}

// ── Colonne gauche : en-tête, arbre, cartes ────────────────────────────────
function renderHead() {
  const s = S.snap;
  $('version').textContent = s.version.replace('Claude-Launcher-', '');
  $('tplver').textContent = '· tpl ' + s.tpl;
  const al = $('alerts'); al.textContent = '';
  for (const a of s.alerts || []) {
    const d = el('div', 'alert ' + a.color + (a.click ? ' click' : '') + (a.notify ? ' notify' : '')); // notify : annonce seule (CL_UPDATE_MODE=notify), ni clic ni installation
    const t = el('span', 'txt', a.text); t.title = a.text;
    if (a.click) t.onclick = () => (a.kind === 'hubcfg' ? diagBox() : call('alert', { kind: a.kind }));
    const x = el('span', 'x', '✕'); x.title = 'masquer (Ctrl+U)'; x.onclick = () => call('dismiss', { kind: a.kind });
    if (a.kind === 'github') d.append(t); else d.append(t, x); // alerte GitHub (quota / injoignable) : non masquable (#199)
    al.appendChild(d);
  }
  const u = $('usage'); u.textContent = '';
  if (!s.usage || !s.usage.length) {
    u.appendChild(el('span', 'msg', 'consommation Claude : ' + (s.usageMsg || 'chargement…')));
  } else {
    const col = { blue: 'var(--blue)', yellow: 'var(--yellow)', red: 'var(--red)', dim: 'var(--dim)' };
    for (const r of s.usage) {
      const lab = el('span', 'lab', r.label); lab.title = r.label;
      const bar = el('div', 'bar'); const f = el('div'); f.style.width = (r.passed ? 0 : r.pct) + '%'; f.style.background = col[r.tone] || col.blue; bar.appendChild(f);
      const pct = el('span', 'pct t-' + (r.tone === 'blue' ? 'blue' : r.tone), r.passed ? '—' : r.pct + ' %');
      const rst = el('span', 'rst', r.passed ? 'réinit. passée' : r.reset);
      u.append(lab, bar, pct, rst);
    }
    if (s.stale) u.appendChild(el('span', 'msg t-yellow', s.stale));
  }
  const lg = $('login');
  lg.classList.toggle('hidden', !s.login);
  lg.textContent = s.login || '';
  lg.className = 'login ' + (s.loginBad ? 'bad' : 'warn') + (s.login ? '' : ' hidden');
  $('filter').textContent = S.filter ? 'filtre › ' + S.filter + '▏' : 'Filtrer (frappe)';
  $('filter').className = S.filter ? '' : 'dim';
  $('filterbox').classList.toggle('on', !!S.filter);
  $('status').textContent = s.status || '';
  $('status').title = s.perf ? 'chargement : ' + s.perf : '';
  $('msg').textContent = quitText() || s.msg || '';
}

function quitText() {
  if (S.quit === 1) return '[l] launcher seul (projets gardés) · [t] tout fermer · autre : annuler';
  if (S.quit === 2) return `Fermer ${(S.snap.tabs || []).length} projet(s) et leur claude ? [o] oui · autre : annuler`;
  return '';
}

// ── Arbre des projets (#122 : rendu d'un arbre natif Windows 11) ───────────
// Lignes à la hauteur et à l'indentation de l'Explorateur, chevron des
// racines et catégories, pastille, nom, release, puis les badges dans des
// colonnes fixes (une par sorte d'icône : local, remote, template, conteneur,
// milestone, PR, reprise) pour qu'ils s'alignent d'une ligne à l'autre.
const SLOTS = ['local', 'remote', 'tpl', 'ctr', 'ms', 'pr', 'resume'];

// level : niveau dans l'arbre (racine 0, catégorie et projet selon leur profondeur).
const level = (r) => (r.kind === 'root' ? 0 : r.depth + 1);

// visibleRows : arbre, ou projets filtrés (sans hiérarchie, comme la coque).
function visibleRows() {
  const rows = (S.snap && S.snap.rows) || [];
  if (S.filter) {
    const f = S.filter.toLowerCase();
    return rows.filter((r) => r.kind === 'project' && r.name.toLowerCase().includes(f));
  }
  // sous une racine ou catégorie repliée (#123) : masqué jusqu'à la ligne
  // suivante de même niveau ou de niveau supérieur
  const out = [];
  let hide = Infinity;
  for (const r of rows) {
    const lv = level(r);
    if (lv <= hide) hide = Infinity;
    if (hide !== Infinity) continue;
    out.push(r);
    if (r.kind !== 'project' && isCollapsed(r.path)) hide = lv;
  }
  return out;
}
function projectRows() { return visibleRows().filter((r) => r.kind === 'project'); }

// ── Repli des racines et catégories (#123), mémorisé dans le cache du launcher
const prefs = {
  v: {}, timer: null, pending: {},
  get(k, d) { return this.v[k] === undefined ? d : this.v[k]; },
  set(k, val) {
    this.v[k] = val; this.pending[k] = val;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { const p = this.pending; this.pending = {}; call('setPrefs', p).catch(() => {}); }, 300);
  },
};
function isCollapsed(path) { return (prefs.get('collapsed', [])).includes(path); }
function setCollapsed(path, on) {
  const c = prefs.get('collapsed', []).filter((x) => x !== path);
  if (on) c.push(path);
  prefs.set('collapsed', c);
  renderTree();
}
function toggleNode(path) { setCollapsed(path, !isCollapsed(path)); }
function rowOf(path) { return ((S.snap && S.snap.rows) || []).find((r) => r.path === path); }
// parentOf : racine ou catégorie qui contient la ligne visible path.
function parentOf(path) {
  const rows = visibleRows();
  const i = rows.findIndex((r) => r.path === path);
  if (i < 0) return null;
  const lv = level(rows[i]);
  for (let j = i - 1; j >= 0; j--) if (level(rows[j]) < lv) return rows[j].path;
  return null;
}

function badgeCells(r) {
  const cols = el('span', 'cols');
  const by = {};
  for (const b of r.badges || []) by[b.slot || 'x'] = b;
  for (const sl of SLOTS) {
    const b = by[sl];
    const c = el('span', 'c c-' + sl + (b ? ' ' + tone(b.tone) : ''), b ? b.icon + (b.short ? ' ' + b.short : '') : '');
    if (b) c.title = b.text;
    cols.appendChild(c);
  }
  if (!r.gitOK) cols.firstChild.textContent = '…';
  return cols;
}

function renderTree() {
  const tree = $('tree'); tree.textContent = '';
  const rows = visibleRows();
  if (!rows.length) tree.appendChild(el('div', 'row empty dim', 'aucun projet'));
  for (const r of rows) {
    const d = el('div', 'row ' + r.kind);
    d.dataset.path = r.path;
    d.setAttribute('role', 'treeitem');
    d.setAttribute('aria-level', String(level(r) + 1));
    const ind = el('span', 'ind'); ind.style.width = 'calc(var(--indent) * ' + (S.filter ? 0 : level(r)) + ')'; // #132
    d.appendChild(ind);
    if (r.kind !== 'project') {
      const chev = el('span', 'chev' + (isCollapsed(r.path) ? '' : ' open'));
      chev.innerHTML = '<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M3.5 1.5 7 5 3.5 8.5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg>';
      d.setAttribute('aria-expanded', String(!isCollapsed(r.path)));
      const nm = el('span', 'name', r.name);
      nm.title = r.path;
      d.append(chev, nm);
      if (r.missing) d.classList.add('missing');
    } else {
      d.appendChild(el('span', 'chev none'));
      const dot = el('span', 'dot ' + tone(r.closing ? { color: 'cyan' } : r.dot), r.closing ? '⏻' : (r.dot ? r.dot.icon : '○'));
      dot.title = r.closing || (r.dot ? r.dot.text : '');
      const name = el('span', 'name', r.name);
      name.title = r.path;
      d.append(dot, name, el('span', 'rel', r.release || ''), badgeCells(r));
      if (r.open) {
        d.classList.add('open');
        d.style.setProperty('--pc', r.color || '#1F4FD1');
      }
    }
    if (r.path === S.cursor) { d.classList.add('cur'); d.setAttribute('aria-selected', 'true'); }
    tree.appendChild(d);
  }
  $('newproj').classList.toggle('cur', S.cursor === 'new');
  const cur = tree.querySelector('.row.cur');
  if (cur) cur.scrollIntoView({ block: 'nearest' });
}

// ── Rafraîchir un projet, bandeau d'étapes (#158) ───────────────────────────
// Un bandeau par projet rafraîchi, en tête de ses cartes : une coche par étape de l'évènement `refresh`.
// Fin sans erreur : il disparaît ; avec erreur : il reste en rouge jusqu'à × ou au rafraîchissement suivant.
const REFRESH_LABELS = { ctr: 'conteneur / pod', session: 'session', git: 'git', github: 'GitHub', tpl: 'template', usage: 'consommation' };
const REFRESH_IDS = ['ctr', 'session', 'git', 'github', 'tpl', 'usage'];
const refreshBands = {};
function refreshBand(path) {
  const b = refreshBands[path];
  if (!b) return null;
  const err = b.done && b.steps.some((s) => s.state === 'err');
  const d = el('div', 'steps' + (err ? ' err' : ''));
  const head = el('div', 'sthead');
  head.append(el('b', '', err ? 'Rafraîchi, avec une erreur' : '⟳ Rafraîchissement de ' + (b.name || '')));
  if (err) { const x = el('span', 'stx', '×'); x.title = 'fermer'; x.onclick = (e) => { e.stopPropagation(); delete refreshBands[path]; renderCards(); }; head.append(x); }
  const line = el('div', 'stline');
  for (const s of b.steps) {
    const ic = { ok: ['✓', 't-green'], run: ['⟳', 't-cyan'], err: ['✕', 't-red'] }[s.state] || ['·', 't-dim'];
    line.append(el('span', ic[1], ic[0]), el('span', '', ' ' + (REFRESH_LABELS[s.id] || s.id) + '  '));
  }
  d.append(head, line);
  if (err && b.msg) d.title = b.msg;
  return d;
}
function onRefresh(ev) {
  if (!ev || !ev.path) return;
  const r = rowOf(ev.path);
  const name = r ? r.name : ev.path.split('/').pop();
  const err = (ev.steps || []).some((s) => s.state === 'err');
  if (ev.done && !err) delete refreshBands[ev.path];
  else refreshBands[ev.path] = { name, steps: ev.steps || [], done: !!ev.done, msg: ev.msg || '' };
  if (ev.done && ev.msg) flashBrief(name + ' : ' + ev.msg, 6000);
  if (ev.path === S.cursor) renderCards();
}
async function refreshProject(path) {
  const r0 = rowOf(path);
  // bandeau posé avant l'appel : un évènement `done` arrivé avant le retour du call le remplace/l'efface correctement
  const prev = refreshBands[path];
  refreshBands[path] = { name: r0 ? r0.name : '', steps: REFRESH_IDS.map((id) => ({ id, state: 'wait' })), done: false, msg: '' };
  if (path === S.cursor) renderCards();
  let r;
  try { r = await call('refreshProject', { path }); } catch (e) { r = { ok: false, msg: e.message }; }
  if (r && r.ok === false) {
    if (prev) refreshBands[path] = prev; else delete refreshBands[path];
    if (path === S.cursor) renderCards();
    flash(r.msg || 'rafraîchissement impossible');
  }
}

let teamClick = { key: null, at: 0 };
function renderCards() {
  const c = S.cards, box = $('cards');
  box.textContent = '';
  if (!c || !c.path) { box.appendChild(el('div', 'dim', 'Sélectionnez un projet dans le navigateur.')); return; }
  box.dataset.path = c.path; // le titre et les cartes désignent toujours ce projet (#131)
  if (c.loading) {
    const t = el('div', 'cardstitle'); t.append(el('span', '', '◈ ÉTAT · ' + c.name));
    box.append(t, el('div', 'dim cardsload', 'chargement…'));
    const bl = refreshBand(c.path); if (bl) box.insertBefore(bl, t.nextSibling);
    return;
  }
  const pend = new Set(c.pending || []);
  const hidden = prefs.get('cardsHidden', false);
  const t = el('div', 'cardstitle');
  const fold = el('span', 'fold' + (hidden ? '' : ' open'), '›');
  fold.title = hidden ? 'afficher les cartes' : 'masquer les cartes';
  t.onclick = (e) => { if (e.target.closest('.cfglink')) return; e.stopPropagation(); setCardsHidden(!hidden); };
  t.append(fold, el('span', '', '◈ ÉTAT · ' + c.name));
  const ce = el('span', 'cfglink', '⚙ env.yaml'); ce.title = 'environnement du projet (.claude-launcher/env.yaml)'; ce.onclick = (e) => { e.stopPropagation(); configEditor(c.path, 'project-env'); };
  if (c.updated) t.append(el('span', c.staleUpd ? 't-yellow' : '', '· mis à jour ' + c.updated));
  t.append(el('span', 'sp'), ce);
  box.appendChild(t);
  const rb = refreshBand(c.path); if (rb) box.appendChild(rb);
  box.classList.toggle('folded', hidden);
  // carte repliable d'un clic sur son bandeau (#124), état mémorisé
  const card = (title, sub, body, chip) => {
    const closed = !!prefs.get('cardsClosed', {})[title];
    const k = el('div', 'card' + (closed ? ' closed' : ''));
    const h = el('div', 'ch');
    h.append(el('span', 'fold' + (closed ? '' : ' open'), '›'), el('span', '', title));
    if (Array.isArray(sub)) { // parties { t, warn } : ⚠ orange après la partie, détail en infobulle (#199)
      sub.forEach((p) => { h.append(el('span', 'dim', ' · ' + p.t)); if (p.warn) { const w = el('span', 't-orange', ' ⚠'); w.title = p.warn; h.append(w); } });
    } else if (sub) h.append(el('span', 'dim', ' · ' + sub));
    if (chip) { const p = el('span', 'hidechip', chip); p.title = 'les teammates inactifs sont cachés après ce délai (Réglages › Launcher › teams)'; h.append(p); }
    h.onclick = (e) => { e.stopPropagation(); const m = Object.assign({}, prefs.get('cardsClosed', {})); if (m[title]) delete m[title]; else m[title] = true; prefs.set('cardsClosed', m); renderCards(); };
    k.append(h);
    if (!closed) k.append(body);
    if (!hidden) box.appendChild(k);
    return k;
  };
  // Projet : légende des icônes de la ligne, puis git et versions
  const pb = el('div', 'cb');
  const lg = el('div', 'legend');
  for (const i of c.legend || []) lg.append(el('span', 'i ' + tone(i.tone), i.icon), el('span', 't', i.text));
  pb.appendChild(lg);
  if (!c.loaded) pb.appendChild(el('div', 'dim', pend.has('état') ? 'lecture du projet…' : 'préparation en cours…'));
  else if (!c.isRepo) pb.appendChild(el('div', 'dim', 'pas un dépôt git'));
  else {
    const kv = el('div', 'kv');
    const rel = el('span'); for (const s of c.release || []) rel.append(el('span', s.tone ? 't-' + s.tone : '', s.text));
    kv.append(el('span', '', 'branche'), el('span', '', c.branch), el('span', '', 'version'), el('span', '', 'locale ' + c.verLocal),
      el('span', '', ''), (() => { const s = el('span', '', 'release '); s.append(rel); return s; })(),
      el('span', '', 'dernier'), el('span', '', c.lastMsg || ''));
    kv.lastChild.title = c.lastMsg || '';
    pb.appendChild(kv);
  }
  card('Projet', '', pb);
  // Milestone
  const m = c.milestone;
  if (m) {
    const mb = el('div', 'cb');
    const pctOf = (a, b) => (b > 0 ? Math.round(a * 100 / b) : 0);
    const bar = (a, b) => { const x = el('div', 'msbar'); const f = el('div'); f.style.width = pctOf(a, b) + '%'; x.appendChild(f); return x; };
    if (m.kind === 'working' || m.kind === 'releasing') {
      const h = el('div', 'mshead');
      h.append(el('span', m.kind === 'working' ? 't-yellow' : 't-orange', '⚑'), el('span', 't-cyan b', m.title));
      if (m.kind === 'releasing') h.append(el('span', 't-orange', '— à publier'));
      h.append(el('span', 'sp'), el('span', 'dim', `${m.closed}/${m.total} · ${pctOf(m.closed, m.total)} %`));
      mb.append(h, bar(m.closed, m.total));
      if (m.phases && m.phases.length) {
        const ph = el('div'); m.phases.forEach((p, i) => { if (i) ph.append(el('span', 'dim', ' · ')); ph.append(el('span', p.tone ? 't-' + p.tone : '', p.text)); });
        mb.appendChild(ph);
      }
      if (m.blocked) mb.appendChild(el('div', 't-red b', `⛔ ${m.blocked} bloquée(s)`));
    } else if (m.kind === 'idle') {
      mb.appendChild(el('div', 'dim', '⚑ aucun milestone en cours'));
      for (const r of m.pending || []) {
        const row = el('div', 'msrow'); const tt = el('span', 'tt', r.title); tt.title = r.title;
        const b = bar(r.closed, r.total); b.style.margin = '0';
        row.append(tt, el('span', 'dim', `${r.closed}/${r.total}`), b, el('span', 'b', pctOf(r.closed, r.total) + '%'));
        mb.appendChild(row);
      }
      if (m.more) mb.appendChild(el('div', 'dim', `  … ${m.more} autre(s)`));
    } else if (m.kind === 'done') {
      const d = el('div'); d.append(el('span', 't-green', '⚑ ' + m.title + ' publié'), el('span', 'dim', ' (il y a ' + m.doneAgo + ')'));
      mb.appendChild(d);
    }
    card('Milestone', m.readAgo ? 'lu ' + m.readAgo : '', mb);
  }
  // Team
  const tb = el('div', 'cb');
  if ((!c.team || !c.team.length) && pend.has('team')) tb.appendChild(el('div', 'dim', 'lecture de la team…'));
  else if (!c.team || !c.team.length) tb.appendChild(el('div', 'dim', c.open ? 'Pas de team template.' : 'Pas de team template (Entrée pour lancer).'));
  else {
    // une ligne par teammate (#136) : ● démarré / ○ non ; surbrillance = pane visible ;
    // double-clic (450 ms, détecté à la main : la carte est redessinée à chaque évènement)
    const g = el('div', 'team');
    for (const m of c.team) {
      const on = !!m.started || !!m.claude;
      const r = el('div', 'tr' + (on && !m.hidden ? ' vis' : '') + (on ? '' : ' off'));
      // état du template (#136) : ● vert à jour, ▲ orange en retard (○ pour un non démarré, mêmes couleurs) ; neutre : comme avant
      // #151 : la version n'a plus de colonne (elle est dans le titre de la carte) ; ⚠ orange en dernière colonne
      // quand la ligne s'écarte de l'en-tête (m.odd : tpl, claude), détail en infobulle
      const late = m.tplState === 'late', ok = m.tplState === 'ok';
      const odd = m.odd || [];
      const mark = el('span', ok || on ? 't-green' : 't-dim', on ? '●' : '○');
      const warn = el('span', 't-orange', odd.length ? '⚠' : '');
      r.append(mark, el('span', 'n', (m.companion ? '+ ' : '') + m.name),
        el('span', 'm', (m.model || '—') + ' · ' + (m.effort || '—')), warn);
      const st = !on ? 'non démarré' : m.name === 'leader' ? 'leader' : m.active ? 'actif' : m.hidden ? 'caché' : m.pinned ? 'visible (fixé)' : 'visible' + (m.idleLeft ? ' · ' + m.idleLeft + ' s' : '');
      const tip = [st];
      if (m.tplState) tip.push('template ' + (m.tag || '?') + (m.tplTarget ? ' — cible ' + m.tplTarget : ' — version cible inconnue') + (late ? ' (en retard)' : ' (à jour)'));
      else if (m.tag && !m.tplTarget) tip.push('template ' + m.tag + ' — version cible inconnue');
      if (m.tplState || m.tag || m.tplTarget) tip.push(m.companion ? 'compagnon : oui' + (m.companionFile ? ' (' + m.companionFile + ')' : '') : 'compagnon : non');
      if (odd.includes('tpl')) tip.push('⚠ template ' + (m.tag || '?') + ' ≠ ' + ((c.teamHead && c.teamHead.tpl) || '?'));
      if (odd.includes('claude')) tip.push('⚠ claude ' + (m.claude || '?') + ' ≠ ' + ((c.teamHead && c.teamHead.claude) || '?'));
      if (on && m.claude) tip.push('claude ' + m.claude);
      r.title = tip.join('\n');
      if (on && m.pane) r.onclick = () => {
        const now = Date.now(), k = c.path + '|' + m.pane;
        if (teamClick.key === k && now - teamClick.at < 450) {
          teamClick = { key: null, at: 0 };
          call('teamToggle', { path: c.path, pane: m.pane }).then((res) => flash(res && res.ok === false ? (res.msg || 'refusé') : '')).catch(() => {});
        } else teamClick = { key: k, at: now };
      };
      g.appendChild(r);
    }
    tb.appendChild(g);
  }
  // titre « Team · N · tpl vX · claude Y » (#151) : versions de l'en-tête (teamHead), omises si inconnues
  const th = c.teamHead || {};
  const vt = (s) => (/^v/.test(s) ? s : 'v' + s);
  const sub = c.team && c.team.length ? [{ t: String(c.team.length) }].concat(
    th.tpl ? [{ t: 'tpl ' + vt(th.tpl), warn: th.tplNewer ? 'template du projet ' + vt(th.tpl) + ' — ' + vt(th.tplNewer) + ' disponible (/init-project au lancement)' : '' }] : [],
    th.claude ? [{ t: 'claude ' + th.claude }] : []) : '';
  // puce « masquage N s » (#188) : seulement quand le masquage automatique est actif (activé et délai > 0) ; aucune puce sinon
  const hi = th.hideIdle;
  card('Team', sub, tb, hi && hi.enabled && hi.delay > 0 ? 'masquage ' + hi.delay + ' s' : '');
  // Fichiers
  if (c.loaded) {
    const fb = el('div', 'cb'); const g = el('div', 'files');
    const mark = (ch) => (!ch ? ['', ''] : ch === 'D' ? ['t-red', ch] : (ch === '?' || ch === 'A') ? ['t-green', ch] : ['t-yellow', ch]);
    for (const f of c.files || []) {
      const [cls, ch] = mark(f.change);
      const n = el('span', 'n' + (f.dir ? ' dir' : ''), f.name + (f.dir ? '/' : '')); n.title = f.name;
      g.append(el('span', cls, ch), n, el('span', 'dim', f.meta));
    }
    if (!c.files || !c.files.length) g.append(el('span'), el('span', 'dim', '(vide)'), el('span'));
    fb.appendChild(g);
    card('Fichiers', String((c.files || []).length), fb);
  }
}

// ── Navigation au clavier et à la souris ───────────────────────────────────
function select(path, opts) {
  if (!path) return;
  S.cursor = path;
  const row = rowOf(path);
  if (path !== 'new' && row && row.kind === 'project') {
    showCardsOf(path, row.name);
    call('select', { path }).then((c) => takeCards(c)).catch(() => {});
    prefetchNeighbors(path);
    const v = S.views[path];
    if (v && opts && opts.showTab) showView(path);
  }
  renderTree();
}

// ── Cartes du projet sélectionné : jamais celles d'un autre (#131) ─────────
// Au clic, les cartes précédentes disparaissent aussitôt : celles du projet en
// cache local (survol, voisins, visite précédente), sinon « chargement… » ;
// toute réponse porte son projet et n'est affichée que s'il est encore choisi.
const cardsCache = {};
function showCardsOf(path, name) {
  S.cards = cardsCache[path] || { path, name, loading: true };
  renderCards();
}
function takeCards(c) {
  if (!c || !c.path) return;
  cardsCache[c.path] = c;
  if (c.path === S.cursor) { S.cards = c; renderCards(); }
}
// préchargement (#130) : projet survolé, voisins de la sélection
function prefetchCards(path) {
  if (!path || cardsCache[path]) return;
  call('cardsFor', { path }).then((c) => { if (c && c.path && !cardsCache[c.path]) cardsCache[c.path] = c; }).catch(() => {});
}
function prefetchNeighbors(path) {
  const ps = projectRows().map((r) => r.path);
  const i = ps.indexOf(path);
  [ps[i - 1], ps[i + 1]].forEach((p) => prefetchCards(p));
}
let hoverTimer = null;
$('tree').addEventListener('mouseover', (e) => {
  const row = e.target.closest('.row.project');
  clearTimeout(hoverTimer);
  if (row) hoverTimer = setTimeout(() => prefetchCards(row.dataset.path), 150);
});

function move(d) {
  const ps = visibleRows().map((r) => r.path).concat(['new']);
  let i = ps.indexOf(S.cursor);
  if (i < 0) i = 0; else i = Math.max(0, Math.min(ps.length - 1, i + d));
  select(ps[i]);
}

async function openProject(path, mode) {
  if (path === 'new') return newProject('');
  const r = await call('open', { path });
  if (!r) return;
  if (r.execChoice) return execChoice(r.execChoice, true);
  if (r.tab) showView(r.tab, true);
}

async function askClose(path) {
  const r = await call('close', { path });
  if (!r) return;
  if (r.msg) flash(r.msg);
  if (r.question) {
    confirmBox(r.question.replace(/ · o\/N$/, ''), 'Fermer', async () => { await call('closeConfirm', { path }); }, true);
  }
}

// Forcer la fermeture (#158) : boîte de confirmation (raison, ce qui est nettoyé).
async function askForceClose(path) {
  const r = await call('forceClose', { path });
  if (!r) return;
  if (r.msg) flash(r.msg);
  if (!r.question) return;
  openModal((b) => {
    b.append(el('h2', '', r.question.replace(/ · o\/N$/, '')));
    if (r.why) b.append(el('p', 'why', r.why + '. L\'état local sera nettoyé :'));
    if ((r.items || []).length) { const ul = el('ul', 'fcitems'); for (const i of r.items) ul.append(el('li', '', i)); b.append(ul); }
    const btns = el('div', 'btns');
    const no = el('button', 'btn', 'Annuler'); no.onclick = closeModal;
    const yes = el('button', 'btn danger', 'Forcer la fermeture');
    yes.onclick = async () => {
      closeModal();
      const c = await call('forceCloseConfirm', { path });
      if (c && c.msg) flash(c.msg);
    };
    btns.append(no, yes); b.appendChild(btns);
    no.focus(); // défaut sûr : Entrée annule ; Échap aussi (gestion générale des modales)
  });
}

$('tree').addEventListener('click', (e) => {
  const row = e.target.closest('.row'); $('nav').focus(); S.focusNav = true;
  if (!row || !row.dataset.path) { showView('leaders'); return; }
  const path = row.dataset.path;
  const now = Date.now();
  const dbl = S.lastClick.key === path && now - S.lastClick.at < 450;
  S.lastClick = { key: path, at: now };
  if (!row.classList.contains('project')) {
    // double-clic : créer un projet ici (comme la coque) ; clic : replier / déplier
    if (dbl) { S.lastClick.key = null; if (row.classList.contains('missing')) flash('⚠ ' + path + ' inaccessible (montage perdu ?)'); else newProject(path); return; }
    S.cursor = path;
    toggleNode(path);
    return;
  }
  if (e.ctrlKey || e.metaKey) { select(path); askClose(path); return; }
  select(path);
  const r = S.snap.rows.find((x) => x.path === path);
  if (dbl || (r && r.open)) { S.lastClick.key = null; openProject(path); }
});
// ── Menu contextuel d'un projet (#148, LOT-3.2) ────────────────────────────
function closeCtx() { const m = $('ctxmenu'); if (m) m.remove(); }
function showProjectMenu(path, x, y) {
  closeCtx();
  const r = (S.snap.rows || []).find((v) => v.path === path) || {};
  const m = el('div', 'ctxmenu'); m.id = 'ctxmenu';
  const it = (label, key, fn, dis) => {
    const d = el('div', dis ? 'dis' : '', label);
    if (key) d.appendChild(el('span', 'k', key));
    if (!dis) d.onclick = (e) => { e.stopPropagation(); closeCtx(); fn(); };
    m.appendChild(d);
  };
  it('Ouvrir', 'Entrée', () => openProject(path));
  // Fermer : selon l'état de la ligne (ouvert ou pastille autre que ○), plus selon la présence d'un onglet (#158)
  it('Fermer', 'Suppr', () => askClose(path), !(r.open || (r.dot && r.dot.icon && r.dot.icon !== '○')));
  it('⟳ Rafraîchir', 'F5', () => refreshProject(path), !!r.refreshing);
  it('Forcer la fermeture…', '', () => askForceClose(path), !r.canForce);
  m.appendChild(el('hr'));
  it('⚙ Réglages du projet…', '', () => openSettings(path, 'projet'));
  it('Changer de backend…', 'F2', () => openSettings(path, 'projet', 'pjf-backend'));
  it('Environnement du conteneur…', '', () => openSettings(path, 'projet', 'pjf-env'));
  it('Couleur…', '', () => openSettings(path, 'projet', 'pjf-color'));
  m.appendChild(el('hr'));
  it('Éditer env.yaml en YAML', '', () => configEditor(path, 'project-env'));
  document.body.appendChild(m);
  const w = m.offsetWidth, h = m.offsetHeight;
  m.style.left = Math.max(0, Math.min(x, window.innerWidth - w - 4)) + 'px';
  m.style.top = Math.max(0, Math.min(y, window.innerHeight - h - 4)) + 'px';
}
$('tree').addEventListener('contextmenu', (e) => {
  const row = e.target.closest('.row.project');
  if (!row || !row.dataset.path) return;
  e.preventDefault(); e.stopPropagation();
  select(row.dataset.path);
  showProjectMenu(row.dataset.path, e.clientX, e.clientY);
});
document.addEventListener('mousedown', (e) => { if (!e.target.closest('#ctxmenu')) closeCtx(); });
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeCtx(); }, true);
window.addEventListener('blur', closeCtx);

$('newproj').addEventListener('click', () => { select('new'); newProject(''); });
$('login').addEventListener('click', () => call('login').then(() => {}).catch(() => {}));
$('cards').addEventListener('click', () => $('nav').focus());

document.addEventListener('keydown', (e) => {
  if (!$('modal').classList.contains('hidden')) { if (e.key === 'Escape') { if (CE.doc) ceClose(); else if (typeof ST !== 'undefined' && ST.on) stClose(); else closeModal(); } return; }
  const inTerm = e.target && e.target.closest && e.target.closest('.xterm');
  if (inTerm) return; // le terminal garde ses touches (raccourcis : attachCustomKeyEventHandler)
  if (S.quit) { onQuitKey(e); return; }
  const k = e.key;
  const handled = () => { e.preventDefault(); e.stopPropagation(); };
  if (e.ctrlKey && (k === 'ArrowLeft' || k === 'ArrowRight')) { resizeNav(k === 'ArrowLeft' ? -32 : 32); return handled(); }
  if (e.ctrlKey && k === 'Tab') { cycleTab(e.shiftKey ? -1 : 1); return handled(); }
  switch (k) {
    case 'ArrowUp': move(-1); return handled();
    case 'ArrowDown': move(1); return handled();
    case 'PageUp': move(-10); return handled();
    case 'PageDown': move(10); return handled();
    case 'ArrowLeft': {
      const r = rowOf(S.cursor);
      if (r && r.kind !== 'project' && !isCollapsed(r.path) && !S.filter) setCollapsed(r.path, true);
      else { const par = parentOf(S.cursor); if (par) select(par); }
      return handled();
    }
    case 'ArrowRight': {
      const r = rowOf(S.cursor);
      if (r && r.kind !== 'project' && !S.filter) { if (isCollapsed(r.path)) setCollapsed(r.path, false); else move(1); }
      return handled();
    }
    case 'Enter': {
      const r = rowOf(S.cursor);
      if (r && r.kind !== 'project') toggleNode(r.path); else if (S.cursor) openProject(S.cursor);
      return handled();
    }
    case 'Delete': if (isProject(S.cursor)) askClose(S.cursor); return handled();
    case 'F1': helpBox(); return handled();
    case 'F2': if (isProject(S.cursor)) execF2(S.cursor); return handled();
    case 'F5': call('refresh'); if (isProject(S.cursor)) refreshProject(S.cursor); return handled();
    case 'Tab': focusTerminal(); return handled();
    case 'Escape':
      if (S.filter) { S.filter = ''; renderTree(); renderHead(); return handled(); }
      if (webOfNative()) { flash('quitter : réservé à la fenêtre native'); return handled(); }
      S.quit = 1; renderHead(); return handled();
    case 'Backspace':
      if (S.filter) { S.filter = S.filter.slice(0, -1); afterFilter(); }
      return handled();
  }
  if (e.ctrlKey && !e.altKey) {
    const l = k.toLowerCase();
    if (l === 'r') { call('rescan'); return handled(); }
    if (l === 'u') { call('dismiss', { kind: 'all' }); return handled(); }
    if (l === 'd') { diagBox(); return handled(); }
    if (k === ',') { openSettings(S.cursor && S.cursor !== 'new' ? S.cursor : ''); return handled(); }
    if (l === 'c') { S.quit = 1; renderHead(); return handled(); }
    return;
  }
  if (k.length === 1 && !e.altKey && !e.metaKey) { S.filter += k; afterFilter(); return handled(); }
});

function isProject(path) { const r = rowOf(path); return !!(r && r.kind === 'project'); }

function afterFilter() {
  const ps = projectRows();
  if (ps.length && !ps.find((r) => r.path === S.cursor)) select(ps[0].path);
  renderTree(); renderHead();
}

function onQuitKey(e) {
  e.preventDefault();
  const k = e.key.toLowerCase(), step = S.quit;
  S.quit = 0;
  if (step === 1 && k === 'l') { call('quit', { mode: 'l' }); flash('fermeture du launcher (projets conservés)…'); return; }
  if (step === 1 && k === 't') {
    if (!(S.snap.tabs || []).length) { call('quit', { mode: 't' }); return; }
    S.quit = 2; renderHead(); return;
  }
  if (step === 2 && (k === 'o' || k === 'y')) { call('quit', { mode: 't' }); return; }
  renderHead();
}

// largeur du navigateur : bordure glissée ou Ctrl+← Ctrl+→ (mémorisée)
function setNavW(w, keep) {
  w = Math.max(280, Math.min(window.innerWidth - 300, w));
  document.documentElement.style.setProperty('--navw', w + 'px');
  if (!keep) prefs.set('navW', Math.round(w));
  scheduleFit();
}
// hauteur de l'arbre (séparateur horizontal arbre / cartes, #124)
function setTreeH(h, keep) {
  const nav = $('nav').getBoundingClientRect().height;
  h = Math.max(90, Math.min(nav - 160, h));
  document.documentElement.style.setProperty('--treeh', h + 'px');
  document.body.classList.add('treeh');
  if (!keep) prefs.set('treeH', Math.round(h));
}
function setCardsHidden(on) {
  prefs.set('cardsHidden', on);
  document.body.classList.toggle('cardsfolded', on);
  renderCards();
}
function resizeNav(d) { setNavW($('nav').getBoundingClientRect().width + d); }
(() => {
  const sp = $('splitter'); let drag = false;
  sp.addEventListener('mousedown', (e) => { drag = true; sp.classList.add('drag'); e.preventDefault(); });
  window.addEventListener('mousemove', (e) => { if (drag) setNavW(e.clientX); });
  window.addEventListener('mouseup', () => { if (drag) { drag = false; sp.classList.remove('drag'); } });
  const hs = $('hsplit'); let hdrag = false;
  hs.addEventListener('mousedown', (e) => { hdrag = true; hs.classList.add('drag'); e.preventDefault(); });
  window.addEventListener('mousemove', (e) => { if (hdrag) setTreeH(e.clientY - $('tree').getBoundingClientRect().top); });
  window.addEventListener('mouseup', () => { if (hdrag) { hdrag = false; hs.classList.remove('drag'); } });
})();
// tailles mémorisées (cache du launcher), appliquées à l'initialisation
// thème (#133) : Auto (celui de Windows, suivi en direct), Jour, Nuit ; couvre
// toute l'interface, l'éditeur et le thème des terminaux (xterm.js)
const darkMQ = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
function setTheme(t, keep) {
  if (t === 'light' || t === 'dark') document.documentElement.dataset.theme = t;
  else delete document.documentElement.dataset.theme;
  if (!keep) prefs.set('theme', t);
  call('setTheme', { mode: t }).catch(() => {}); // barre de titre de la fenêtre (Wails)
  const seg = $('themeseg'); seg.textContent = '';
  for (const [v, label, tip] of [['auto', 'Auto', 'celui de Windows'], ['light', '☀', 'Jour'], ['dark', '☾', 'Nuit']]) {
    const b = el('button', t === v ? 'on' : '', label); b.title = 'Thème : ' + tip;
    b.onclick = (e) => { e.stopPropagation(); setTheme(v); };
    seg.appendChild(b);
  }
  retheme();
}
if (darkMQ) darkMQ.addEventListener('change', () => retheme());
// termTheme : couleurs des terminaux sans couleur de pane (ouverture, login,
// vignettes en attente), tirées du thème
function termTheme() {
  const cs = getComputedStyle(document.documentElement);
  const v = (n) => cs.getPropertyValue(n).trim();
  return { background: v('--input'), foreground: v('--text'), cursor: v('--accent'), selectionBackground: v('--selbg-focus') };
}
function retheme() {
  const th = termTheme();
  for (const v of Object.values(S.views)) v.launch.options.theme = Object.assign({}, v.launch.options.theme, th);
}
function applyLayoutPrefs() {
  setTheme(prefs.get('theme', 'auto'), true);
  const w = prefs.get('navW', 0); if (w) setNavW(w, true);
  const h = prefs.get('treeH', 0); if (h) setTreeH(h, true);
  FONT_SIZE = clampFont(prefs.get('termFontSize', FONT_DEFAULT));
  setZoneSize('tree', prefs.get('treeFontSize', TREE_DEFAULT), true);
  setZoneSize('cards', prefs.get('cardsFontSize', CARDS_DEFAULT), true);
  document.body.classList.toggle('cardsfolded', !!prefs.get('cardsHidden', false));
}

// ── Onglets ────────────────────────────────────────────────────────────────
function renderTabs() {
  const bar = $('tabs'); bar.textContent = '';
  // Teamleaders (#162) : pastille à contour épinglée à gauche, hors de la zone qui défile, suivie d'un filet, sans ×
  const lt = el('div', 'tl' + (S.view === 'leaders' ? ' on' : ''));
  lt.append(el('span', 'tlic', '⌗'), el('span', 'tltx', ' Teamleaders'));
  lt.onclick = () => showView('leaders');
  const scroll = el('div', 'tscroll');
  bar.append(lt, el('div', 'tsep'), scroll, layoutButton());
  for (const t of (S.snap && S.snap.tabs) || []) {
    const d = el('div', 'tab'); d.dataset.path = t.path;
    const on = S.view === t.path;
    if (on) { d.classList.add('on'); d.style.background = t.color || '#1F4FD1'; }
    else if (t.busy) { d.classList.add('busy'); d.style.background = lighten(t.color || '#C99A4B'); }
    d.append(el('span', '', t.name + (t.busy && !on ? ' · agent au travail' : '')));
    if (t.state === 'opening') d.append(el('span', 'st', '⏳'));
    if (t.state === 'error') d.append(el('span', 'st t-red', '⚠'));
    if (t.readOnly) d.append(el('span', 'st', '👁'));
    const x = el('span', 'x', '×'); x.title = 'détacher (la session continue dans tmux)';
    x.onclick = (e) => { e.stopPropagation(); detachTab(t.path); };
    d.appendChild(x);
    d.onclick = () => showView(t.path, true);
    scroll.appendChild(d);
  }
  fitLeadersTab();
}
// Bouton de mise en page (#153) : tout à droite, hors de la zone qui défile ; bascule le projet de l'onglet actif entre
// « leader en haut » et « leader à gauche » et enregistre l'exception du projet (projectApply layout). L'organisation
// effective vient de projectSettings (layout), relue au plus toutes les 5 s et après chaque bascule ; grisé sans projet.
const layoutCache = {}; // chemin -> { v: 'top'|'left', at }
const layoutLoading = {};
const LAYOUT_TXT = { top: 'leader en haut', left: 'leader à gauche' };
function layoutDrop(path) { if (path) { delete layoutCache[path]; delete layoutFail[path]; } else { for (const k of Object.keys(layoutCache)) delete layoutCache[k]; for (const k of Object.keys(layoutFail)) delete layoutFail[k]; } layoutLastPath = ''; }
function layoutIcon(v) {
  const sv = '<svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.2">';
  const body = v === 'left' ? '<rect x="1.5" y="2.5" width="13" height="11" rx="1.5"/><rect x="1.5" y="2.5" width="5.5" height="11" fill="currentColor" stroke="none"/><line x1="7" y1="8" x2="14.5" y2="8"/>'
    : '<rect x="1.5" y="2.5" width="13" height="11" rx="1.5"/><rect x="1.5" y="2.5" width="13" height="5" fill="currentColor" stroke="none"/><line x1="8" y1="7.5" x2="8" y2="13.5"/>';
  return sv + body + '</svg>';
}
// Relecture : au changement d'onglet actif (force), après bascule / enregistrement des Réglages (layoutDrop), sinon au plus toutes
// les 30 s ; sur erreur, attente croissante (2 s, 4 s … 60 s) — jamais un appel par instantané.
const layoutFail = {}; // chemin -> { n, until }
let layoutLastPath = '';
function layoutFetch(path, force) {
  if (layoutLoading[path]) return;
  const now = Date.now(), c = layoutCache[path], f = layoutFail[path];
  if (f && now < f.until) return;
  if (!force && c && now - c.at < 30000) return;
  layoutLoading[path] = true;
  transport.call('projectSettings', { path }).then((r) => {
    if (r && r.layout) { delete layoutFail[path]; const old = layoutCache[path] && layoutCache[path].v; layoutCache[path] = { v: r.layout, at: Date.now() }; if (old !== r.layout) paintLayoutButton(); }
    else throw new Error('sans layout');
  }).catch(() => { const n = ((layoutFail[path] || {}).n || 0) + 1; layoutFail[path] = { n, until: Date.now() + Math.min(60000, 1000 * Math.pow(2, n)) }; }).finally(() => { delete layoutLoading[path]; });
}
function layoutButton() {
  const b = el('div', 'tlayb'); b.id = 'tlayb';
  b.onclick = () => toggleLayout();
  paintLayoutButton(b);
  return b;
}
function paintLayoutButton(b) {
  b = b || $('tlayb'); if (!b) return;
  const path = S.view !== 'leaders' ? S.view : '';
  const cur = path && layoutCache[path] ? layoutCache[path].v : '';
  b.classList.toggle('dis', !path || !cur);
  b.setAttribute('aria-disabled', !path || !cur ? 'true' : 'false'); // grisé mais l'infobulle reste visible : le clic est ignoré (toggleLayout)
  b.innerHTML = layoutIcon(cur === 'left' ? 'left' : 'top');
  if (!path) b.title = 'Pas de mise en page pour l\'onglet Teamleaders : choisir un onglet de projet';
  else if (!cur) b.title = 'Mise en page : lecture en cours…';
  else b.title = 'Mise en page : ' + LAYOUT_TXT[cur] + ' — cliquer pour ' + LAYOUT_TXT[cur === 'left' ? 'top' : 'left'];
  if (path) { const changed = path !== layoutLastPath; layoutLastPath = path; layoutFetch(path, changed); } else layoutLastPath = '';
}
async function toggleLayout() {
  const path = S.view !== 'leaders' ? S.view : '';
  const cur = path && layoutCache[path] && layoutCache[path].v;
  if (!path || !cur) return;
  const next = cur === 'left' ? 'top' : 'left';
  layoutCache[path] = { v: next, at: Date.now() }; paintLayoutButton();
  let r;
  try { r = await call('projectApply', { path, layout: next }); } catch (e) { r = null; }
  if (!r || r.ok === false) { layoutDrop(path); paintLayoutButton(); if (r && r.msg) flash('⚠ ' + r.msg); return; }
  flashBrief('Mise en page : ' + LAYOUT_TXT[next], 3000);
}

// Réduction à l'icône (#162) : les onglets de projets sont mesurés contre la place restante **avec la pastille à sa
// largeur complète** (mesurée pastille étendue) — sinon réduire ; pas d'oscillation réduit/étendu.
function fitLeadersTab() {
  const bar = $('tabs'), lt = bar.querySelector('.tl'), sc = bar.querySelector('.tscroll'), sep = bar.querySelector('.tsep');
  if (!lt || !sc) return;
  lt.classList.remove('ico'); lt.removeAttribute('title');
  const full = lt.offsetWidth + parseFloat(getComputedStyle(lt).marginLeft || 0) + parseFloat(getComputedStyle(lt).marginRight || 0);
  const lb = bar.querySelector('.tlayb'), lbw = lb ? lb.offsetWidth + parseFloat(getComputedStyle(lb).marginLeft || 0) + parseFloat(getComputedStyle(lb).marginRight || 0) : 0;
  const sp = sep.offsetWidth + parseFloat(getComputedStyle(sep).marginLeft || 0) + parseFloat(getComputedStyle(sep).marginRight || 0);
  if (sc.scrollWidth > bar.clientWidth - full - sp - lbw + 1) { lt.classList.add('ico'); lt.title = 'Teamleaders'; }
  const on = sc.querySelector('.tab.on'); if (on) on.scrollIntoView({ block: 'nearest', inline: 'nearest' });
}
window.addEventListener('resize', () => fitLeadersTab());
// largeur de la barre changée sans évènement de fenêtre (séparateur du panneau gauche…) : recalcul, sans boucle
// (la hauteur et les changements de pastille ne comptent pas : seule la largeur observée déclenche)
if (typeof ResizeObserver === 'function') {
  let tabsW = -1;
  new ResizeObserver((es) => {
    const w = Math.round(es[0].contentRect.width);
    if (w === tabsW) return;
    tabsW = w;
    fitLeadersTab();
  }).observe($('tabs'));
}
function lighten(hex) {
  const n = parseInt(hex.slice(1), 16); const f = (v) => Math.min(255, Math.round(v + (255 - v) * 0.45));
  return '#' + [f(n >> 16), f((n >> 8) & 255), f(n & 255)].map((v) => v.toString(16).padStart(2, '0')).join('');
}
function cycleTab(d) {
  const ids = ['leaders'].concat(((S.snap && S.snap.tabs) || []).map((t) => t.path));
  const i = ids.indexOf(S.view);
  showView(ids[(i + d + ids.length) % ids.length], true);
}
function detachTab(path) {
  call('detach', { path });
  dropView(path);
  if (S.view === path) showView('leaders');
}

function showView(id, focus) {
  if (id !== 'leaders' && !S.views[id]) ensureView(id);
  S.view = id;
  for (const [p, v] of Object.entries(S.views)) v.root.classList.toggle('hidden', p !== id);
  leaders.root.classList.toggle('hidden', id !== 'leaders');
  renderTabs();
  if (id === 'leaders') leaders.layout();
  else { const v = S.views[id]; v.fit(); if (focus) v.focus(); }
  if (id !== 'leaders' && S.cursor !== id) { S.cursor = id; call('select', { path: id }); renderTree(); }
}

function focusTerminal() {
  S.focusNav = false;
  if (S.view === 'leaders') leaders.focus(); else if (S.views[S.view]) S.views[S.view].focus();
}

// ── Vue d'un projet : terminal d'ouverture puis panes ──────────────────────
function ensureView(path) {
  if (S.views[path]) return S.views[path];
  const v = new ProjView(path);
  S.views[path] = v;
  $('views').appendChild(v.root);
  call('termSync', { path });
  return v;
}
function dropView(path) {
  const v = S.views[path]; if (!v) return;
  v.dispose(); delete S.views[path];
  leaders.drop(path);
}

class ProjView {
  constructor(path) {
    this.path = path;
    this.root = el('div', 'view hidden');
    this.launchBox = el('div', 'launch');
    this.launchEl = el('div', 'lt');
    this.launchBox.appendChild(this.launchEl);
    this.panesEl = el('div', 'panes hidden');
    this.banner = el('div', 'banner hidden');
    this.prefixEl = el('div', 'prefix hidden', 'Ctrl+B …');
    this.root.append(this.launchBox, this.panesEl, this.banner, this.prefixEl);
    this.panes = {};        // id -> {box, head, term}
    this.layout = null;
    this.state = 'opening';
    this.pending = {};      // sortie arrivée avant le pane
    this.launch = newTerm({ convertEol: true, cursorBlink: true, theme: termTheme() });
    this.launch.open(this.launchEl);
    this.launch.onData((d) => call('launchInput', { path: this.path, data: d }));
    const lclip = { term: this.launch, readOnly: () => !!this.ro };
    this.launch.attachCustomKeyEventHandler((ev) => terminalKey(ev, lclip));
    attachClip(lclip);
    this.focusPane = null;
    this.sent = null;
    this.borders();
  }
  // bordures entre panes (#135) : glisser une bordure redimensionne le pane
  // (resize-pane), comme la souris dans la coque. Bordure verticale : la
  // colonne libre entre deux panes ; horizontale : la ligne d'en-tête du pane
  // du dessous (un clic sans glisser y garde son effet : focus, zoom).
  borders() {
    const at = (e) => {
      if (!this.layout || !cell) return null;
      const r = this.panesEl.getBoundingClientRect();
      const k = this.panesEl.offsetWidth ? r.width / this.panesEl.offsetWidth : 1;
      const cx = (e.clientX - r.left) / k / cell.w, cy = ((e.clientY - r.top) / k - cell.h) / cell.h;
      return { cx, cy, b: paneBorder(this.layout.panes, cx, cy, e.target === this.panesEl) };
    };
    this.panesEl.addEventListener('mousemove', (e) => {
      if (this.drag) return;
      const h = at(e);
      this.panesEl.style.cursor = h && h.b ? (h.b.axis === 'x' ? 'col-resize' : 'row-resize') : '';
    });
    this.panesEl.addEventListener('mousedown', (e) => {
      const h = at(e);
      if (!h || !h.b || e.button !== 0) return;
      if (e.target === this.panesEl) e.preventDefault();
      const d = this.drag = { b: h.b, x0: e.clientX, y0: e.clientY, on: false, last: 0 };
      const move = (ev) => {
        if (!d.on && Math.abs(ev.clientX - d.x0) + Math.abs(ev.clientY - d.y0) < cell.w / 2) return;
        if (!d.on) { d.on = true; document.body.classList.add('pdrag'); }
        const g = at(ev);
        const n = d.b.axis === 'x' ? Math.round(g.cx) - d.b.pane.x : Math.round(g.cy) - d.b.pane.y;
        if (n >= 1 && n !== d.last) { d.last = n; call('resizePane', { path: this.path, pane: d.b.pane.id, kind: d.b.axis, cols: n }); }
      };
      const up = () => {
        window.removeEventListener('mousemove', move, true); window.removeEventListener('mouseup', up, true);
        document.body.classList.remove('pdrag');
        if (d.on) { const stop = (ev) => { ev.stopPropagation(); ev.preventDefault(); }; window.addEventListener('click', stop, { capture: true, once: true }); setTimeout(() => window.removeEventListener('click', stop, true), 0); }
        this.drag = null;
      };
      window.addEventListener('mousemove', move, true); window.addEventListener('mouseup', up, true);
    });
  }
  dispose() { this.launch.dispose(); for (const p of Object.values(this.panes)) p.term.dispose(); this.root.remove(); }
  status(m) {
    if (m.replay != null) { this.launch.reset(); this.launch.write(m.replay); }
    this.state = m.state;
    const ready = m.state === 'ready';
    const pty = m.state === 'pty';
    this.launchBox.classList.toggle('hidden', ready);
    this.panesEl.classList.toggle('hidden', !ready);
    this.launchBox.style.inset = pty ? '4px' : '';
    this.ro = !!m.readOnly;
    this.banner.classList.toggle('hidden', !m.readOnly);
    if (m.readOnly) this.banner.textContent = 'lecture seule : un autre launcher a la main — Entrée sur le projet pour la reprendre';
    this.fit();
  }
  // fit : zone des terminaux → taille demandée au client tmux (cellules)
  // fit : zone des terminaux → taille demandée au client tmux (cellules) ;
  // calculée sur la zone commune des vues (vue masquée comprise : la session
  // d'un projet ouvert en arrière-plan a tout de suite la bonne taille).
  fit() {
    const vr = $('views').getBoundingClientRect();
    if (!vr.width) return;
    if (!cell) cell = measureCell();
    this.fitLaunch(vr);
    if (this.state === 'pty') return;
    const cols = Math.max(20, Math.floor((vr.width - 8) / cell.w));
    const rows = Math.max(6, Math.floor((vr.height - 8 - cell.h) / cell.h)); // 1 ligne d'en-têtes en haut
    const key = cols + 'x' + rows;
    if (!hasKeyboard()) return; // sans le clavier : géométrie reçue affichée telle quelle (applyLayout), la nôtre n'est pas renvoyée
    if (this.sent !== key) { this.sent = key; call('termResize', { path: this.path, cols, rows }); }
  }
  fitLaunch(vr) {
    vr = vr || $('views').getBoundingClientRect();
    if (!vr.width) return;
    if (!cell) cell = measureCell();
    const m = this.state === 'pty' ? 8 + 14 : 16 + 14; // marges de la boîte et du terminal
    const cols = Math.max(20, Math.floor((vr.width - m) / cell.w)), rows = Math.max(5, Math.floor((vr.height - m) / cell.h));
    if (this.launch.cols !== cols || this.launch.rows !== rows) this.launch.resize(cols, rows);
    if (this.state === 'pty' && hasKeyboard()) call('termResize', { path: this.path, cols, rows });
  }
  focus() {
    if (this.state !== 'ready') { this.launch.focus(); return; }
    const id = this.focusPane && this.panes[this.focusPane] ? this.focusPane : (this.layout && this.layout.leader);
    if (id && this.panes[id]) this.panes[id].term.focus();
  }
  applyLayout(l) {
    this.layout = l;
    if (!cell) cell = measureCell();
    // fenêtre plus grande que la zone (un autre launcher pilote la taille, #126) :
    // affichée réduite plutôt que coupée
    const vr = $('views').getBoundingClientRect();
    const k = Math.min(1, (vr.width - 8) / (l.cols * cell.w), (vr.height - 8) / ((l.rows + 1) * cell.h));
    this.panesEl.style.transformOrigin = '0 0';
    this.panesEl.style.transform = k < 0.999 ? 'scale(' + k + ')' : '';
    const seen = {};
    for (const p of l.panes) {
      seen[p.id] = true;
      let pv = this.panes[p.id];
      if (!pv) pv = this.panes[p.id] = this.makePane(p);
      const top = cell.h + p.y * cell.h; // ligne d'en-têtes au-dessus
      Object.assign(pv.box.style, { left: p.x * cell.w + 'px', top: top + 'px', width: p.w * cell.w + 'px', height: p.h * cell.h + 'px', display: '' });
      Object.assign(pv.head.style, { left: p.x * cell.w + 'px', top: (top - cell.h) + 'px', width: p.w * cell.w + 'px', height: cell.h + 'px', display: '' });
      pv.head.style.setProperty('--pbg', p.bg || '#1F4FD1'); // en-tête clair, couleur du pane à gauche
      pv.head.querySelector('.n').textContent = p.name || p.id;
      pv.head.querySelector('.r').textContent = p.role === 'leader' ? '· leader' : p.role ? '· ' + p.role : '';
      pv.head.classList.toggle('focus', !!p.active);
      // #152 : bandeau selon panes[].state (lead, active, idle, vide), plus déduit du texte de role
      pv.head.classList.toggle('active', p.state === 'active');
      pv.head.classList.toggle('idle', p.state === 'idle');
      if (pv.term.cols !== p.w || pv.term.rows !== p.h) pv.term.resize(p.w, p.h);
      if (pv.bg !== p.bg) { pv.bg = p.bg; pv.term.options.theme = Object.assign({}, pv.term.options.theme, { background: p.bg }); pv.box.style.background = p.bg; }
      if (p.active) this.focusPane = p.id;
    }
    for (const [id, pv] of Object.entries(this.panes)) {
      if (!seen[id]) { pv.box.style.display = 'none'; pv.head.style.display = 'none'; }
    }
    // panes disparus pour de bon (fenêtre relue sans eux, hors zoom)
    if (!l.zoomed) for (const id of Object.keys(this.panes)) if (!seen[id]) { this.panes[id].term.dispose(); this.panes[id].box.remove(); this.panes[id].head.remove(); delete this.panes[id]; }
    leaders.update(this.path, l);
  }
  // zoom (#138) : bascule locale immédiate (le pane prend toute la zone, ou
  // la mise en page d'avant revient), puis tmux (resize-pane -Z) : la mise en
  // page et la recapture du pane suivent et corrigent.
  zoom(id) {
    const l = this.layout;
    if (l && this.state === 'ready') {
      if (!l.zoomed) {
        const p = l.panes.find((q) => q.id === id);
        if (p) {
          this.unzoomed = l;
          this.applyLayout(Object.assign({}, l, { zoomed: true, panes: [Object.assign({}, p, { x: 0, y: 0, w: l.cols, h: l.rows, active: true })] }));
        }
      } else if (this.unzoomed) {
        this.applyLayout(Object.assign({}, this.unzoomed, { zoomed: false }));
      }
    }
    call('termZoom', { path: this.path, pane: id });
  }
  makePane(p) {
    const box = el('div', 'pane'), x = el('div', 'xt'); box.appendChild(x);
    const head = el('div', 'phead'); head.append(el('span', 'n'), el('span', 'r'));
    this.panesEl.append(box, head);
    const term = newTerm({ cols: p.w, rows: p.h });
    term.open(x);
    refineCell(term);
    term.onData((d) => this.input(p.id, d));
    term.onBinary((d) => termIn({ path: this.path, pane: p.id, data: d, binary: true })); // souris X10 (#135)
    const clip = { term, readOnly: () => !!this.ro };
    term.attachCustomKeyEventHandler((ev) => terminalKey(ev, clip));
    attachClip(clip);
    term.textarea && term.textarea.addEventListener('focus', () => { this.focusPane = p.id; S.focusNav = false; call('termFocus', { path: this.path, pane: p.id }); });
    head.addEventListener('dblclick', () => this.zoom(p.id));
    head.addEventListener('click', () => term.focus());
    const pv = { box, head, term, bg: null };
    if (this.pending[p.id]) { for (const c of this.pending[p.id]) this.write(pv, c); delete this.pending[p.id]; }
    return pv;
  }
  write(pv, c) { if (c.reset && !c.keep) pv.term.reset(); if (c.data) pv.term.write(c.data); }
  out(chunks) {
    for (const c of chunks) {
      const pv = this.panes[c.pane];
      if (pv) this.write(pv, c); else (this.pending[c.pane] = this.pending[c.pane] || []).push(c);
      leaders.out(this.path, c);
    }
  }
  input(pane, d) {
    if (S.prefix) {
      S.prefix = false; this.prefixEl.classList.add('hidden');
      if (d === 'z') return this.zoom(pane);
      if (d === 'R' || d === 'r') return call('relayout', { path: this.path });
      if (d === '\x02') return termIn({ path: this.path, pane, data: '\x02' });
      if (d === '\x16') return termIn({ path: this.path, pane, data: '\x16' }); // Ctrl+B Ctrl+V : ^V seul, sans préfixe tmux
      return termIn({ path: this.path, pane, data: '\x02' + d });
    }
    if (d === '\x02') { S.prefix = true; this.prefixEl.classList.remove('hidden'); return; }
    termIn({ path: this.path, pane, data: d });
  }
}

// paneBorder : bordure de pane sous la souris (cellules, cy : 0 = première
// ligne des panes, -1 = ligne d'en-têtes) ; gap : la souris est hors de tout
// pane (colonne libre entre deux panes). Renvoie {axis, pane} : le pane à
// gauche (« x ») ou au-dessus (« y ») de la bordure, celui qu'on redimensionne.
function paneBorder(panes, cx, cy, gap) {
  const col = Math.floor(cx), row = Math.floor(cy);
  for (const p of panes) {
    if (gap && col === p.x + p.w && row >= p.y - 1 && row < p.y + p.h) {
      if (panes.some((q) => q.x === col + 1)) return { axis: 'x', pane: p };
    }
  }
  for (const p of panes) {
    if (row === p.y + p.h && col >= p.x && col < p.x + p.w) {
      if (panes.some((q) => q.y === row + 1 && q.x <= col && col < q.x + q.w)) return { axis: 'y', pane: p };
    }
  }
  return null;
}

// terminalKey : raccourcis de l'application pris dans un terminal (false : pas pour lui).
function terminalKey(ev, clip) {
  if (ev.type !== 'keydown') return true;
  if (!$('modal').classList.contains('hidden')) return false; // boîte ouverte : rien ne part au terminal (#158)
  if (clip) {
    // Ctrl+Maj+C copie la sélection (Ctrl+C reste l'interruption) ; Ctrl+V, Ctrl+Maj+V et Maj+Inser collent par
    // ce seul chemin (preventDefault : pas de second collage par l'évènement natif « paste »)
    const k = ev.key;
    if (ev.ctrlKey && ev.shiftKey && !ev.altKey && (k === 'C' || k === 'c')) { ev.preventDefault(); clipCopy(clip.term); return false; }
    // Ctrl+B puis Ctrl+V : ^V brut au programme (collage d'image de Claude Code, vim, nano) — préfixe armé : on laisse xterm l'envoyer
    if (S.prefix && ev.ctrlKey && !ev.shiftKey && !ev.altKey && (k === 'V' || k === 'v')) { ev.preventDefault(); return true; }
    if ((ev.ctrlKey && !ev.altKey && (k === 'V' || k === 'v')) || (ev.shiftKey && !ev.ctrlKey && k === 'Insert')) { ev.preventDefault(); clipPaste(clip); return false; }
  }
  if (ev.ctrlKey && ev.key === 'Tab') { ev.preventDefault(); cycleTab(ev.shiftKey ? -1 : 1); return false; }
  if (ev.altKey && (ev.key === 'ArrowLeft' || ev.key === 'ArrowRight') && !ev.ctrlKey) { ev.preventDefault(); $('nav').focus(); S.focusNav = true; return false; }
  if (ev.ctrlKey && ev.shiftKey && (ev.key === 'W' || ev.key === 'w') && S.view !== 'leaders') { ev.preventDefault(); detachTab(S.view); return false; }
  if (ev.key === 'F1' && ev.shiftKey) { ev.preventDefault(); helpBox(); return false; }
  return true;
}

// ── Onglet ⌗ Teamleaders : vignette interactive du leader de chaque projet ─
const leaders = {
  root: (() => { const r = el('div', 'view'); r.append(el('div', 'leaders')); return r; })(),
  tiles: {}, // projet -> {tile, body, term, pane}
  get grid() { return this.root.firstChild; },
  update(path, l) {
    let t = this.tiles[path];
    const lp = l.panes.find((p) => p.id === l.leader);
    if (!lp) return;
    if (t && t.pane !== l.leader) { this.drop(path); t = null; }
    if (!t) {
      const tab = (S.snap.tabs || []).find((x) => x.path === path) || { name: path.split(/[\\/]/).pop(), color: '#1F4FD1' };
      const tile = el('div', 'tile'), th = el('div', 'th'), body = el('div', 'tb'), cl = el('div', 'clone');
      th.append(el('span', '', tab.name), el('span', 'dim', '· team-lead'));
      th.style.background = tab.color;
      body.appendChild(cl); tile.append(th, body);
      this.grid.appendChild(tile);
      const term = newTerm({ cols: lp.w, rows: lp.h, cursorBlink: false });
      term.open(cl);
      term.onData((d) => termIn({ path, pane: l.leader, data: d }));
      term.onBinary((d) => termIn({ path, pane: l.leader, data: d, binary: true }));
      const clip = { term, readOnly: () => !!((S.snap.tabs || []).find((x) => x.path === path) || {}).readOnly };
      term.attachCustomKeyEventHandler((ev) => terminalKey(ev, clip));
      attachClip(clip);
      term.textarea && term.textarea.addEventListener('focus', () => tile.classList.add('focus'));
      term.textarea && term.textarea.addEventListener('blur', () => tile.classList.remove('focus'));
      th.addEventListener('dblclick', () => showView(path, true));
      th.addEventListener('click', () => term.focus());
      t = this.tiles[path] = { tile, body, cl, term, pane: l.leader, w: lp.w, h: lp.h };
      call('termSync', { path }); // contenu actuel du leader (capture)
      this.layout();
    }
    if (t.w !== lp.w || t.h !== lp.h) { t.w = lp.w; t.h = lp.h; t.term.resize(lp.w, lp.h); this.scale(t); }
    t.term.options.theme = Object.assign({}, t.term.options.theme, { background: lp.bg });
  },
  out(path, c) { const t = this.tiles[path]; if (t && c.pane === t.pane) { if (c.reset && !c.keep) t.term.reset(); if (c.data) t.term.write(c.data); } },
  drop(path) { const t = this.tiles[path]; if (!t) return; t.term.dispose(); t.tile.remove(); delete this.tiles[path]; this.layout(); },
  layout() {
    const n = Object.keys(this.tiles).length;
    const g = this.grid;
    g.querySelectorAll('.empty').forEach((e) => e.remove());
    if (!n) {
      const e = el('div', 'empty'); e.innerHTML = 'Aucun projet ouvert.<br>Entrée ou double-clic sur un projet pour l\'ouvrir ; ses teamleaders apparaîtront ici.';
      g.appendChild(e); g.style.gridTemplateColumns = '1fr'; return;
    }
    const cols = Math.ceil(Math.sqrt(n)), rows = Math.ceil(n / cols);
    g.style.gridTemplateColumns = `repeat(${cols}, minmax(0, 1fr))`;
    g.style.gridTemplateRows = `repeat(${rows}, minmax(0, 1fr))`;
    requestAnimationFrame(() => { for (const t of Object.values(this.tiles)) this.scale(t); });
  },
  scale(t) {
    if (!cell) cell = measureCell();
    const r = t.body.getBoundingClientRect();
    if (!r.width) return;
    const k = Math.min(1, r.width / (t.w * cell.w), r.height / (t.h * cell.h));
    t.cl.style.transform = `scale(${k})`;
  },
  focus() { const f = Object.values(this.tiles)[0]; if (f) f.term.focus(); },
};
$('views').appendChild(leaders.root);

// ── Fenêtres modales ───────────────────────────────────────────────────────
function openModal(build) {
  const box = $('modalbox'); box.textContent = '';
  build(box);
  $('modal').classList.remove('hidden');
  const f = box.querySelector('input, select, .btn.pri, .btn');
  if (f) f.focus();
}
function closeModal() { if (typeof stKeepClear === 'function') stKeepClear(); if (typeof ST !== 'undefined') { ST.on = false; clearTimeout(ST.timer); } $('modal').classList.add('hidden'); $('nav').focus(); }
$('modal').addEventListener('mousedown', (e) => { if (e.target === $('modal')) { if (CE.doc) ceClose(); else if (typeof ST !== 'undefined' && ST.on) stClose(); else closeModal(); } });

function confirmBox(text, ok, fn, danger) {
  openModal((b) => {
    b.append(el('h2', '', text));
    const btns = el('div', 'btns');
    const no = el('button', 'btn', 'Annuler (N)'); no.onclick = closeModal;
    const yes = el('button', 'btn ' + (danger ? 'danger' : 'pri'), ok + ' (O)'); yes.onclick = () => { closeModal(); fn(); };
    btns.append(no, yes); b.appendChild(btns);
    b.onkeydown = (e) => { const k = e.key.toLowerCase(); if (k === 'o' || k === 'y') { e.preventDefault(); yes.click(); } else if (k === 'n') { e.preventDefault(); closeModal(); } };
  });
}

function helpBox() {
  const h = S.help || {};
  openModal((b) => {
    b.append(el('h2', '', 'Aide — commandes et raccourcis'));
    for (const s of h.sections || []) {
      b.append(el('h3', '', s.title));
      const g = el('div', 'helpgrid');
      for (const r of s.rows) g.append(el('span', 'k', r.key), el('span', '', r.desc));
      b.appendChild(g);
    }
    b.append(el('h3', '', 'Légende des icônes'));
    const g = el('div', 'helpgrid');
    for (const l of h.legend || []) g.append(el('span', 'li ' + tone({ color: l.tone, bold: l.bold }), l.icon), el('span', '', l.text));
    b.appendChild(g);
    const btns = el('div', 'btns'); const c = el('button', 'btn pri', 'Fermer'); c.onclick = closeModal; btns.appendChild(c); b.appendChild(btns);
  });
}

async function diagBox() {
  openModal((b) => { b.append(el('h2', '', 'Diagnostic des backends'), el('p', 'dim', 'lecture…')); });
  const txt = await call('diag');
  openModal((b) => {
    b.append(el('h2', '', 'Diagnostic des backends'), el('pre', '', txt));
    const btns = el('div', 'btns'); const c = el('button', 'btn pri', 'Fermer'); c.onclick = closeModal; btns.appendChild(c); b.appendChild(btns);
  });
}

function toolsBox(t, force) {
  if (!t || (!t.missing && !force)) return;
  openModal((b) => {
    b.append(el('h2', '', 'Premier lancement — outils requis'));
    b.append(el('p', '', 'Le launcher pilote les backends par leurs clients en ligne de commande. Ceux qui manquent sont à installer (commande ci-dessous), puis relancer le launcher.'));
    const g = el('div', 'tools');
    for (const x of t.tools || []) {
      g.append(el('span', x.Found ? 't-green' : (x.Optional ? 't-yellow' : 't-red'), x.Found ? '✓' : (x.Optional ? '⚠' : '✗')),
        el('span', 'b', x.Tool),
        (() => {
          const d = el('div');
          d.append(el('div', 'dim', 'requis par : ' + (x.RequiredBy || []).join(', ') + (x.Optional ? ' (facultatif)' : '')));
          if (x.Found) d.append(el('div', 'dim', x.Path));
          else if (x.Install) { const c = el('code', '', x.Install); d.append(c); }
          return d;
        })());
    }
    b.appendChild(g);
    const btns = el('div', 'btns');
    const c = el('button', 'btn pri', 'Continuer'); c.onclick = closeModal;
    btns.appendChild(c); b.appendChild(btns);
  });
}

// création d'un projet : nom, type, remote, puis exécution (hôte ou conteneur)
async function newProject(base) {
  const info = await call('newInfo', {});
  openModal((b) => {
    b.append(el('h2', '', 'Nouveau projet'));
    const f = el('div', 'form');
    let root = null;
    if (!base) {
      root = el('select');
      for (const r of info.roots || []) { const o = el('option', '', r); o.value = r; root.appendChild(o); }
      f.append(el('label', '', 'Racine'), root);
    } else {
      f.append(el('label', '', 'Dans'), el('span', 'dim', base));
    }
    const name = el('input'); name.placeholder = 'nom du dossier';
    const kind = el('select');
    for (const [v, t] of [['g', 'géré : git init, /init-project au démarrage'], ['b', 'brut : dossier et CLAUDE.md minimal']]) { const o = el('option', '', t); o.value = v; kind.appendChild(o); }
    const remote = el('input'); remote.placeholder = 'git@github.com:moi/projet.git (facultatif)';
    f.append(el('label', '', 'Nom'), name, el('label', '', 'Type'), kind, el('label', '', 'Remote git'), remote);
    const ex = execFields(info, f);
    kind.onchange = () => { remote.disabled = kind.value === 'b'; };
    b.appendChild(f);
    const err = el('div', 'err'); b.appendChild(err);
    const btns = el('div', 'btns');
    const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = closeModal;
    const ok = el('button', 'btn pri', 'Créer et ouvrir');
    ok.onclick = async () => {
      err.textContent = '';
      try {
        const r = await transport.call('create', Object.assign({ base: base || (root && root.value) || '', name: name.value, raw: kind.value === 'b', remote: remote.value }, ex()));
        closeModal();
        if (r && r.tab) showView(r.tab, true);
      } catch (e) { err.textContent = e.message; }
    };
    btns.append(cancel, ok); b.appendChild(btns);
    b.onkeydown = (e) => { if (e.key === 'Enter') { e.preventDefault(); ok.click(); } };
  });
  setTimeout(() => { const i = $('modalbox').querySelector('input'); if (i) i.focus(); }, 0);
}

// execFields : choix de l'exécution (hôte, ou socle sur un backend conteneur)
function execFields(info, f) {
  if (info.exec == null) return () => ({});
  const sock = el('select');
  const h = el('option', '', 'hôte (sans conteneur)'); h.value = ''; sock.appendChild(h);
  (info.images || []).forEach((im, i) => { const o = el('option', '', '⬡ ' + info.labels[i]); o.value = im; sock.appendChild(o); });
  sock.selectedIndex = info.exec;
  const be = el('select');
  for (const n of info.backends || []) { const o = el('option', '', n); o.value = n; be.appendChild(o); }
  be.value = info.backend;
  const sync = () => { be.disabled = !sock.value; };
  sock.onchange = sync; sync();
  f.append(el('label', '', 'Exécution'), sock, el('label', '', 'Backend'), be);
  return () => ({ image: sock.value, backend: be.value });
}

function execChoice(info, openNext) {
  openModal((b) => {
    b.append(el('h2', '', 'Exécution de ' + (info.name || '')));
    b.append(el('p', 'dim', openNext ? 'Première ouverture : choisir où le projet tourne (F2 pour reconfigurer plus tard).' : 'Reconfigurer l\'exécution du projet (fermé).'));
    const f = el('div', 'form'); const ex = execFields(info, f); b.appendChild(f);
    const err = el('div', 'err'); b.appendChild(err);
    const btns = el('div', 'btns');
    const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = closeModal;
    const ok = el('button', 'btn pri', openNext ? 'Enregistrer et ouvrir' : 'Enregistrer');
    ok.onclick = async () => {
      try {
        const r = await transport.call('execApply', Object.assign({ path: info.path, openNext }, ex()));
        closeModal();
        if (r && r.tab) showView(r.tab, true);
      } catch (e) { err.textContent = e.message; }
    };
    btns.append(cancel, ok); b.appendChild(btns);
  });
}

async function execF2(path) {
  const r = (S.snap.rows || []).find((x) => x.path === path);
  if (r && r.open) { flash('F2 : projet ouvert — le fermer avant de changer de backend'); return; }
  const info = await call('newInfo', { path });
  if (info.exec == null) { flash('F2 : aucun backend conteneur disponible (claude-launcher-ng2 backends)'); return; }
  execChoice(info, false);
}

// ── Éditeur des fichiers de configuration (#117) ───────────────────────────
// Texte brut, numéros de ligne ; validation par core à la frappe (même code
// que le chargement réel) : les erreurs bloquent l'enregistrement, pas les
// avertissements ; .bak, écriture atomique, conflit si le fichier a changé.
const CE = { project: '', files: [], doc: null, dirty: false, timer: null };

async function configEditor(project, kind) {
  CE.project = project || ''; CE.fromPage = '';
  CE.files = await call('configFiles', { path: CE.project });
  openModal((b) => {
    b.classList.add('wide');
    b.innerHTML = '';
    const head = el('div', 'cehead');
    head.append(el('h2', '', '⚙ Fichiers YAML'), el('span', 'sp'));
    const close = el('button', 'btn', 'Fermer (Échap)'); close.onclick = () => ceClose();
    const back = el('button', 'btn', '← Réglages'); back.id = 'ceback'; back.title = 'Revenir à la fenêtre Réglages'; back.onclick = () => ceBack();
    head.append(back, close);
    const body = el('div', 'cebody');
    const list = el('div', 'celist'); list.id = 'celist';
    const ed = el('div', 'ceed'); ed.id = 'ceed';
    body.append(list, ed);
    b.append(head, body);
    ceRenderList();
  });
  const first = CE.files.find((f) => f.kind === kind) || CE.files[0];
  if (first) ceOpen(first.kind);
}

function ceRenderList() {
  const l = $('celist'); if (!l) return;
  l.textContent = '';
  for (const f of CE.files) {
    const it = el('div', 'ceitem' + (CE.doc && CE.doc.kind === f.kind ? ' on' : ''));
    it.append(el('div', 'b', f.label), el('div', 'cepath', f.path),
      el('div', f.exists ? 'dim' : 't-yellow', f.exists ? 'modifié le ' + new Date(f.modTime).toLocaleString('fr-FR') : 'absent : créé à l\'enregistrement (modèle proposé)'));
    it.onclick = () => { if (!CE.dirty || confirm('Abandonner les modifications en cours ?')) ceOpen(f.kind); };
    l.appendChild(it);
  }
}

async function ceOpen(kind) {
  const d = await call('configOpen', { kind, path: CE.project });
  CE.doc = d; CE.dirty = false;
  ceRenderList();
  const ed = $('ceed'); ed.textContent = '';
  const bar = el('div', 'cebar');
  bar.append(el('span', 'cepath', d.path), el('span', 'sp'));
  const reload = el('button', 'btn', 'Recharger'); reload.onclick = () => { if (!CE.dirty || confirm('Abandonner les modifications ?')) ceOpen(kind); };
  const save = el('button', 'btn pri', 'Enregistrer (Ctrl+S)'); save.id = 'cesave'; save.onclick = () => ceSave(false);
  bar.append(reload, save);
  const wrap = el('div', 'cewrap');
  const gut = el('pre', 'cegut'); gut.id = 'cegut';
  const ta = el('textarea', 'cetext'); ta.id = 'cetext'; ta.spellcheck = false; ta.value = d.content;
  ta.wrap = 'off';
  ta.addEventListener('input', () => { CE.dirty = true; ceGutter(); clearTimeout(CE.timer); CE.timer = setTimeout(ceValidate, 300); });
  ta.addEventListener('scroll', () => { gut.scrollTop = ta.scrollTop; });
  ta.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); ceSave(false); }
    if (e.key === 'Tab') { e.preventDefault(); const s = ta.selectionStart; ta.setRangeText('  ', s, ta.selectionEnd, 'end'); ta.dispatchEvent(new Event('input')); }
    if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); ceClose(); }
  });
  wrap.append(gut, ta);
  const probs = el('div', 'ceprobs'); probs.id = 'ceprobs';
  const status = el('div', 'cestatus'); status.id = 'cestatus';
  ed.append(bar, wrap, probs, status);
  if (d.kind === 'config.conf') ed.appendChild(el('p', 'dim', 'Jeton masqué : laisser GITHUB_TOKEN="__GARDER__" le conserve ; saisir une autre valeur le remplace ; "" le retire.'));
  if (d.kind === 'project-env') {
    const side = el('details', 'ceside');
    side.append(el('summary', '', 'Environnement fusionné (env-template.yaml du template puis ce fichier) — lecture seule'));
    const m = el('pre', 'cemerged'); m.id = 'cemerged'; m.textContent = d.merged || '';
    side.appendChild(m);
    if (d.tplText) {
      const t = el('details', 'ceside'); t.append(el('summary', '', 'env-template.yaml (fourni par le template) — lecture seule'), el('pre', 'cemerged', d.tplText));
      ed.appendChild(t);
    }
    ed.appendChild(side);
  }
  if (d.fromTemp) $('cestatus').textContent = 'Fichier absent : modèle proposé, créé à l\'enregistrement.';
  ceGutter(); ceProblems(d.problems || []);
  ta.focus();
}

function ceGutter() {
  const ta = $('cetext'), g = $('cegut'); if (!ta || !g) return;
  const n = ta.value.split('\n').length;
  let s = ''; for (let i = 1; i <= n; i++) s += i + '\n';
  g.textContent = s;
  g.scrollTop = ta.scrollTop;
}

function ceProblems(ps) {
  const box = $('ceprobs'); if (!box) return;
  box.textContent = '';
  const errs = ps.filter((p) => p.severity === 'error').length;
  $('cesave').disabled = errs > 0;
  $('cesave').title = errs ? 'Corriger les erreurs avant d\'enregistrer (le brouillon reste ici)' : '';
  if (!ps.length) { box.appendChild(el('div', 't-green', '✓ aucun problème')); return; }
  for (const p of ps) {
    const r = el('div', 'ceprob ' + (p.severity === 'error' ? 't-red' : 't-yellow'));
    r.append(el('span', 'b', (p.severity === 'error' ? '✗ ' : '⚠ ') + (p.line ? 'ligne ' + p.line : 'fichier')), el('span', '', ' ' + p.message));
    if (p.line) r.onclick = () => ceGoto(p.line);
    box.appendChild(r);
  }
}

function ceGoto(line) {
  const ta = $('cetext'); const ls = ta.value.split('\n');
  let pos = 0; for (let i = 0; i < line - 1 && i < ls.length; i++) pos += ls[i].length + 1;
  ta.focus(); ta.setSelectionRange(pos, pos + (ls[line - 1] || '').length);
  const lh = parseFloat(getComputedStyle(ta).lineHeight) || 17;
  ta.scrollTop = Math.max(0, (line - 4) * lh);
}

async function ceValidate() {
  const ta = $('cetext'); if (!ta || !CE.doc) return;
  const r = await call('configValidate', { kind: CE.doc.kind, path: CE.project, content: ta.value });
  ceProblems(r.problems || []);
  const m = $('cemerged'); if (m && r.merged != null) m.textContent = r.merged;
}

async function ceSave(force) {
  const ta = $('cetext'); if (!ta || !CE.doc) return;
  if ($('cesave').disabled) return;
  const r = await call('configSave', { kind: CE.doc.kind, path: CE.project, content: ta.value, mtime: CE.doc.mtime });
  if (r.invalid) { ceProblems(r.problems || []); $('cestatus').textContent = 'Erreurs : rien n\'est enregistré.'; return; }
  if (r.conflict) {
    const st = $('cestatus'); st.textContent = '';
    st.append(el('span', 't-orange', 'Le fichier a été modifié sur le disque depuis son ouverture. '));
    const rel = el('button', 'btn', 'Recharger (perdre mes modifications)'); rel.onclick = () => ceOpen(CE.doc.kind);
    const ow = el('button', 'btn danger', 'Écraser avec ma version'); ow.onclick = () => { CE.doc.mtime = r.mtime; ceSave(true); };
    st.append(rel, ow);
    return;
  }
  CE.doc.mtime = r.mtime || ''; CE.doc.fromTemp = false; CE.dirty = false;
  ceProblems(r.problems || []);
  $('cestatus').textContent = '✓ Enregistré (version précédente gardée en .bak). ' + (r.note || '');
  CE.files = await call('configFiles', { path: CE.project });
  ceRenderList();
}

// ← Réglages : retour à la fenêtre de configuration structurée, sur la rubrique
// d'origine (CE.fromPage) ; texte modifié : proposer de l'enregistrer d'abord.
async function ceBack() {
  if (CE.dirty) {
    if (confirm('Le texte a été modifié. Enregistrer avant de revenir aux Réglages ?\n\nOK : enregistrer — Annuler : autre choix')) {
      const sv = $('cesave');
      if (sv && sv.disabled) { // erreurs déjà affichées : l'enregistrement est sans effet
        const st = $('cestatus'); if (st) st.textContent = '';
        st.appendChild(el('span', 't-red', 'Enregistrement impossible : corriger les erreurs ci-dessous.'));
        return;
      }
      await ceSave(false);
      if (CE.dirty) return; // erreurs ou conflit : rester dans l'éditeur
    } else if (!confirm('Revenir aux Réglages SANS enregistrer (modifications perdues) ?')) return;
  }
  const page = CE.fromPage, project = CE.project;
  CE.doc = null; CE.dirty = false; CE.fromPage = '';
  openSettings(project, page || undefined); // recharge le modèle : il montre le fichier tel qu'enregistré
}

function ceClose() {
  if (CE.dirty && !confirm('Fermer sans enregistrer ? Les modifications seront perdues.')) return;
  CE.doc = null; CE.dirty = false; CE.fromPage = '';
  $('modalbox').classList.remove('wide');
  closeModal();
}
$('cfgbtn').addEventListener('click', (e) => { e.stopPropagation(); openSettings(S.cursor && S.cursor !== 'new' ? S.cursor : ''); });

// ── Événements du Go ───────────────────────────────────────────────────────
// ── Notifications (fenêtre en arrière-plan) : agent en attente, MAJ prête ──
const notified = { busy: {}, upd: '' };
function notify(title, body) {
  if (document.hasFocus() || !('Notification' in window)) return;
  const show = () => { try { new Notification(title, { body }); } catch (e) { /* refusé */ } };
  if (Notification.permission === 'granted') show();
  else if (Notification.permission !== 'denied') Notification.requestPermission().then((p) => { if (p === 'granted') show(); });
}
function notifyChanges(s) {
  for (const t of s.tabs || []) {
    if (notified.busy[t.path] && !t.busy) notify(t.name + ' : agents en attente', 'Plus aucun agent ne travaille : la main est à vous.');
    notified.busy[t.path] = t.busy;
  }
  const g = (s.alerts || []).find((a) => a.kind === 'launcher' && a.color === 'green' && a.click);
  if (g && notified.upd !== g.text) notify('Claude Launcher', g.text.replace(' · cliquer pour installer', ' prête : cliquer sur l\'annonce pour l\'installer'));
  notified.upd = g ? g.text : '';
}

function applyState(s) {
  notifyChanges(s);
  S.snap = s;
  if (!S.cursor || (S.cursor !== 'new' && !s.rows.find((r) => r.path === S.cursor))) S.cursor = s.selected || (projectRows()[0] || {}).path || 'new';
  // onglets disparus (session terminée, détachée)
  const open = new Set((s.tabs || []).map((t) => t.path));
  for (const p of Object.keys(S.views)) if (!open.has(p) && !(S.grace && S.grace.pending.has(p))) { dropView(p); if (S.view === p) S.view = 'leaders'; }
  for (const p of open) if (!S.views[p]) ensureView(p);
  if (S.view !== 'leaders' && !open.has(S.view) && !(S.grace && S.grace.pending.has(S.view))) S.view = 'leaders';
  renderHead(); renderTree(); renderTabs(); lanBanner();
  if (typeof dwRefresh === 'function') dwRefresh();
  for (const [p, v] of Object.entries(S.views)) v.root.classList.toggle('hidden', p !== S.view);
  leaders.root.classList.toggle('hidden', S.view !== 'leaders');
  if (S.view === 'leaders') leaders.layout();
  graceStep();
  refusedSync(s.tabs);
}

// Bannière tant que l'interface web écoute hors boucle locale (webUI.lan) : fenêtre native seulement (#163)
function lanBanner() {
  const w = (S.snap && S.snap.webUI) || {};
  let b = $('lanbanner');
  const show = !!(w.lan && w.state === 'on' && !isWebClient());
  if (!show) { if (b) b.remove(); return; }
  if (!b) { b = el('div', 'lanbanner'); b.id = 'lanbanner'; $('main').insertBefore(b, $('tabs')); }
  b.textContent = '⚠ Interface web ouverte au réseau sur ' + w.listen + ' — ' + (w.warn || (w.access === 'none' ? 'sans jeton : toute machine qui atteint cette adresse pilote ce poste' : 'le jeton circule en clair en HTTP : préférer un tunnel SSH ou TLS')) + ' (Réglages › Interface web)';
}

// Version du serveur (#198) : `server{version,boot}` de l'init, gardé à la première réception (celui de la page).
// Fonction pure : 'update' (version différente : page périmée), 'restart' (même version, autre démarrage : le
// serveur a redémarré), 'none' (identique, ou l'un des deux inconnu : serveur ancien).
function serverVersionAction(prev, next) {
  if (!prev || !next) return 'none';
  if (prev.version !== next.version) return 'update';
  if (prev.boot !== next.boot) return 'restart';
  return 'none';
}
const verTxt = (v) => (/^v/.test(String(v || '')) ? v : 'v' + v);
function srvBanner(kind, text, withBtn, withOk) {
  let b = $('srvbanner');
  if (!text) { if (b) b.remove(); return; }
  if (!b) { b = el('div', 'lanbanner'); b.id = 'srvbanner'; $('main').insertBefore(b, $('tabs')); }
  b.className = 'lanbanner srv ' + kind; b.textContent = text;
  if (withBtn || withOk) b.append(el('span', 'sp'));
  if (withBtn) { const r = el('button', 'btn sm', 'Recharger'); r.onclick = () => location.reload(); b.appendChild(r); }
  if (withOk) { const k = el('button', 'btn sm', 'OK'); k.onclick = () => { S.refused = null; b.remove(); }; b.appendChild(k); }
}
// Redémarrage du serveur : il se rattache lui-même, de façon asynchrone, aux sessions tmux vivantes (a.reattach) ;
// le client ne recrée rien. Les vues ouvertes sont gardées (S.grace) le temps que les onglets reviennent dans
// l'état poussé ; chacune est resynchronisée dès que son onglet réapparaît. Fonction pure, testée par node.
const REATTACH_MS = 20000, REATTACH_FALLBACK_MS = 5000;
function reattachPlan(pending, tabs) {
  const open = new Set((tabs || []).filter((t) => !t.state || t.state === 'ready').map((t) => t.path)); // « opening » : termSync serait ignoré
  const ready = [], left = [];
  for (const p of pending) (open.has(p) ? ready : left).push(p);
  return { ready, left };
}
function graceStep() {
  const g = S.grace; if (!g) return;
  const plan = reattachPlan(g.pending, (S.snap && S.snap.tabs) || []);
  for (const p of plan.ready) { g.pending.delete(p); transport.call('termSync', { path: p }).catch(() => {}); }
  if (!g.pending.size) graceEnd(false);
}
function graceEnd(timeout) {
  const g = S.grace; if (!g) return;
  S.grace = null; clearTimeout(g.timer); clearTimeout(g.fb);
  const left = [...g.pending];
  if (timeout) for (const p of left) { dropView(p); if (S.view === p) S.view = 'leaders'; }
  for (const p of Object.keys(leaders.tiles || {})) transport.call('termSync', { path: p }).catch(() => {});
  if (S.srvGen !== g.gen) return; // un autre bandeau (mise à jour) a pris la place : ne pas l'effacer
  const parts = [];
  if (g.refused.length) parts.push('session introuvable — rouvrir le projet : ' + g.refused.map((p) => String(p).split('/').filter(Boolean).pop() || p).join(', '));
  if (left.length && timeout) parts.push(left.length + (left.length > 1 ? ' terminaux non rattachés' : ' terminal non rattaché') + ' — recharger');
  S.refused = g.refused.length ? { paths: new Set(g.refused), gen: g.gen } : null;
  if (parts.length) srvBanner('orange', parts.join(' · '), left.length > 0 && timeout, g.refused.length > 0);
  else srvBanner('', '');
}
// Bandeau « session introuvable » : retiré dès que le projet concerné est rouvert (onglet présent), si aucun autre
// bandeau du serveur ne l'a remplacé entre-temps.
function refusedSync(tabs) {
  const r = S.refused; if (!r) return;
  for (const t of tabs || []) r.paths.delete(t.path);
  if (r.paths.size) return;
  S.refused = null;
  if (S.srvGen === r.gen) srvBanner('', '');
}
// Rattachement refusé par le serveur ({ok:false} : session absente, onglet en erreur) : message clair pour ce projet.
function graceRefuse(p) {
  const g = S.grace; if (!g || !g.pending.has(p)) return;
  g.pending.delete(p); g.refused.push(p);
  dropView(p); if (S.view === p) S.view = 'leaders';
  if (!g.pending.size) graceEnd(false);
}
// Repli à 5 s : une vue qui attend encore son onglet demande un rattachement (une fois) ; jamais de création côté serveur.
function graceFallback() {
  const g = S.grace; if (!g) return;
  for (const p of [...g.pending]) transport.call('reattach', { path: p }).then((r) => { if (r && r.ok === false) graceRefuse(p); else graceStep(); }, () => {});
}
function onServerInfo(next) {
  if (!next) return;
  const act = serverVersionAction(S.server, next);
  console.info('[serveur] avant', S.server, 'après', next, '→', act);
  if (!S.server) { S.server = next; return; }
  if (act === 'update') {
    S.srvGen = (S.srvGen || 0) + 1;
    srvBanner('blue', '↻ Serveur mis à jour (' + verTxt(next.version) + ') — la page affichée date de ' + verTxt(S.server.version), true);
  } else if (act === 'restart') {
    S.server = next;
    const gen = S.srvGen = (S.srvGen || 0) + 1;
    if (S.grace) { clearTimeout(S.grace.timer); clearTimeout(S.grace.fb); }
    S.grace = { pending: new Set(Object.keys(S.views)), refused: [], gen, timer: setTimeout(() => graceEnd(true), REATTACH_MS), fb: setTimeout(graceFallback, REATTACH_FALLBACK_MS) };
    srvBanner('orange', '⟳ Serveur redémarré — reconnexion des terminaux…');
  }
}

function applyInit(r) {
  S.help = r.help; S.tools = r.tools;
  onServerInfo(r.server);
  prefs.v = r.prefs || {};
  applyLayoutPrefs();
  applyState(r.state);
  if (r.cards && r.cards.path) { cardsCache[r.cards.path] = r.cards; if (!S.cursor || S.cursor === r.cards.path) { S.cards = r.cards; renderCards(); } }
}

function onEvent(name, data) {
  switch (name) {
    case 'state': applyState(data); break;
    case 'cards': takeCards(data); break;
    case 'hello': onServerInfo(data && data.server); break; // version et démarrage du serveur à l'ouverture du flux (#198)
    case 'refresh': onRefresh(data); break;
    case 'term': {
      const v = ensureView(data.path);
      switch (data.kind) {
        case 'launch': v.launch.write(data.data); break;
        case 'status': v.status(data); renderTabs(); break;
        case 'layout': v.applyLayout(data.layout); break;
        case 'out': v.out(data.chunks); break;
      }
      break;
    }
  }
}

// Ctrl+molette (#140) : taille des caractères de tous les terminaux. Seule la
// molette avec Ctrl est prise (aucune touche retirée aux terminaux) ; la page
// ne zoome pas (preventDefault).
function allTerms() {
  const a = [];
  for (const v of Object.values(S.views)) { a.push(v.launch); for (const p of Object.values(v.panes)) a.push(p.term); }
  for (const t of Object.values(leaders.tiles)) a.push(t.term);
  return a;
}
function setFontSize(n, keep) {
  n = clampFont(n);
  if (n === FONT_SIZE) return;
  FONT_SIZE = n;
  const terms = allTerms();
  for (const t of terms) t.options.fontSize = n;
  cell = measureCell();
  const redo = () => {
    for (const v of Object.values(S.views)) { v.sent = null; v.fit(); if (v.layout) v.applyLayout(v.layout); }
    leaders.layout();
  };
  redo();
  // cellule réelle d'xterm, connue après son rendu : remesure puis recalcul
  requestAnimationFrame(() => {
    // un terminal visible (offsetParent nul : vue ou vignette masquée, mesure nulle)
    const vis = terms.find((t) => t.element && t.element.offsetParent !== null);
    if (vis) { const o = cell; refineCell(vis); if (!o || o.w !== cell.w || o.h !== cell.h) redo(); }
  });
  if (!keep) prefs.set('termFontSize', n);
  flash('Taille des caractères : ' + n);
}
// Terminaux (#140) : Ctrl+molette sur les vues
$('views').addEventListener('wheel', (e) => {
  if (!e.ctrlKey) return;
  e.preventDefault(); e.stopPropagation();
  wheelStep(e, 'term', setFontSize, () => FONT_SIZE);
}, { passive: false, capture: true });
// Arbre / cartes (#150) : même mécanique, tailles portées par les variables CSS de zone.
const ZONES = {
  tree: { key: 'treeFontSize', css: '--tfs', def: TREE_DEFAULT, label: "Taille de l'arbre : " },
  cards: { key: 'cardsFontSize', css: '--cfs', def: CARDS_DEFAULT, label: 'Taille des cartes : ' },
};
const zoneSize = (z) => (z === 'tree' ? TREE_SIZE : CARDS_SIZE);
function setZoneSize(z, n, keep) {
  const Z = ZONES[z];
  n = clampFont(n, Z.def);
  if (!keep && n === zoneSize(z)) return;
  if (z === 'tree') TREE_SIZE = n; else CARDS_SIZE = n;
  document.documentElement.style.setProperty(Z.css, n + 'px');
  if (keep) return;
  prefs.set(Z.key, n);
  flashBrief(Z.label + n);
}
// Ctrl+molette : un cran par tranche de 50 (pavé tactile / pincement), accumulé par zone
const wheelAccs = new Map();
function wheelStep(e, k, apply, cur) {
  let acc = (wheelAccs.get(k) || 0) + (e.deltaMode === 0 ? e.deltaY : e.deltaY * 50);
  if (Math.abs(acc) < 50) { wheelAccs.set(k, acc); return; }
  const dir = acc < 0 ? 1 : -1;
  wheelAccs.set(k, 0);
  apply(cur() + dir);
}
// toute la colonne gauche : la zone des cartes → cartes, le reste (en-tête, arbre, pied) → arbre
$('nav').addEventListener('wheel', (e) => {
  if (!e.ctrlKey) return;
  e.preventDefault(); e.stopPropagation();
  if ($('cards').contains(e.target)) wheelStep(e, 'cards', (n) => setZoneSize('cards', n), () => CARDS_SIZE);
  else wheelStep(e, 'tree', (n) => setZoneSize('tree', n), () => TREE_SIZE);
}, { passive: false, capture: true });

let fitTimer = null;
function scheduleFit() {
  clearTimeout(fitTimer);
  fitTimer = setTimeout(() => { for (const v of Object.values(S.views)) v.fit(); leaders.layout(); }, 80);
}
window.addEventListener('resize', scheduleFit);
// un client qui prend le clavier fixe la taille : sa géométrie est renvoyée
window.addEventListener('focus', () => { if (!sharedTerms()) return; for (const v of Object.values(S.views)) { v.sent = null; v.fit(); } });

// focus clavier visible seulement au clavier (comme Windows : pas après un clic)
document.addEventListener('keydown', () => document.body.classList.add('kbd'), true);
document.addEventListener('mousedown', () => document.body.classList.remove('kbd'), true);

transport.listen(onEvent);
call('init').then((r) => {
  applyInit(r);
  // outils requis : lus à part (PATH, contexte docker), jamais avant l'affichage (#130)
  call('tools').then((t) => { S.tools = t; if (!store.get('toolsSeen', false) && t && t.missing) { toolsBox(t); store.set('toolsSeen', true); } }).catch(() => {});
  $('nav').focus();
});
