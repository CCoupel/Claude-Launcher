// Claude Launcher — interface native (Wails) ou web (mode --web).
// Sans framework : l'état vient du Go (événements « state », « cards »,
// « term »), l'interface ne fait que l'afficher et renvoyer les actions
// (Dispatch côté Go). Terminaux : xterm.js, un par pane tmux, placés à la
// géométrie de tmux (mise en page des teams de core).
'use strict';

// ── Transport : Wails (window.go) ou HTTP + SSE ────────────────────────────
const SESSION_MSG = 'session expirée — rouvrir le lien d\'accès';
const DENIED_MSG = 'accès refusé : adresse ou origine non admise (voir Réglages › Interface web › Adresse publique)';
// ── Identité (mode proxy, v7.0.0 : #251, #252) ─────────────────────────────
// `user` = { login, role, kind: github|local, session: {duration, refresh}, logout } (mode proxy seulement).
// `access` = { mode, role, deny[], backendsAdminFields } (tous modes du serveur web ; absent en fenêtre native) : seule
// source du grisage — aucune liste de méthodes en dur ici. Le serveur reste seul juge (refus R1).
let whoUser = null, whoAccess = null;
const R1_TIP = 'réservé à un administrateur';
const r1Denied = (m, kind) => !!(whoAccess && whoAccess.deny && (whoAccess.deny.includes(m) || (kind && whoAccess.deny.includes(m + ':' + kind))));
const r1Admin = () => !!(whoAccess && whoAccess.backendsAdminFields); // champs R1 de backends.yaml non modifiables
// grise un contrôle dont la méthode (ou méthode:kind) est refusée à ce client
function r1Gate(x, m, kind) { if (x && r1Denied(m, kind)) { x.disabled = true; x.classList.add('r1off'); if (!x.title) x.title = R1_TIP; } return x; }
function r1Info(r) {
  if (!r) return;
  if (r.access !== undefined) { whoAccess = r.access && r.access.mode ? { mode: r.access.mode, role: r.access.role || '', deny: Array.isArray(r.access.deny) ? r.access.deny : [], backendsAdminFields: !!r.access.backendsAdminFields } : null; }
  if (r.user !== undefined) {
    const u = r.user;
    whoUser = u && u.login ? { login: String(u.login), role: u.role === 'admin' ? 'admin' : 'user', kind: u.kind === 'local' || u.login === 'admin@local' ? 'local' : 'github', logout: typeof u.logout === 'string' ? u.logout : '', session: u.session && typeof u.session === 'object' ? u.session : null } : null;
  }
  if (r.space !== undefined) { const was = noBackend(); whoSpace = r.space && typeof r.space === 'object' ? r.space : null; if (was !== noBackend() && typeof renderTree === 'function' && S.snap) { renderTree(); if (typeof leaders !== 'undefined') leaders.layout(); } }
  renderWho(); r1Apply();
}
const fmtDur = (d) => { const m = /^(\d+)\s*(h|m)$/.exec(String(d || '')); return m ? m[1] + (m[2] === 'h' ? ' h' : ' min') : String(d || ''); };
function sessionText(se) {
  if (!se || !se.duration) return '';
  const rf = String(se.refresh || '');
  return 'session : ' + fmtDur(se.duration) + (rf ? (rf === '1h' ? ', renouvelée chaque heure' : ', renouvelée toutes les ' + fmtDur(rf)) : '');
}
function renderWho() {
  const w = document.getElementById('who'); if (!w) return;
  w.textContent = '';
  w.classList.toggle('hidden', !whoUser);
  if (!whoUser) return;
  w.append(el('span', 'dot', sessionIsLost ? '○' : '●'), document.createTextNode(whoUser.login + ' ▾'));
  w.title = whoUser.login + ' — ' + (whoUser.role === 'admin' ? 'administrateur' : 'utilisateur');
}
function whoMenu(ev) {
  if (ev) ev.stopPropagation();
  if (!whoUser) return;
  const old = document.getElementById('ctxmenu'); if (old) { old.remove(); if (old.dataset.who) return; }
  const m = el('div', 'ctxmenu whomenu'); m.id = 'ctxmenu'; m.dataset.who = '1';
  m.append(el('div', 'dis hd', whoUser.kind === 'local' ? 'Compte local' : 'Connecté avec GitHub'));
  const l = el('div', 'dis'); l.append(el('b', '', whoUser.login), document.createTextNode(' · ' + (whoUser.role === 'admin' ? 'administrateur' : 'utilisateur')));
  m.appendChild(l);
  const st = sessionText(whoUser.session); if (st) m.appendChild(el('div', 'dis hd', st));
  m.appendChild(el('hr'));
  const out = el('div'); out.append(el('span', 'ck', '⎋'), el('span', '', 'Se déconnecter'));
  out.onclick = (e) => { e.stopPropagation(); m.remove(); location.href = whoUser.logout || 'oauth2/sign_out?rd=' + encodeURIComponent(new URL('signed-out', document.baseURI).pathname); };
  m.appendChild(out);
  document.body.appendChild(m);
  const r = $('who').getBoundingClientRect();
  m.style.left = Math.max(0, Math.min(r.left, window.innerWidth - m.offsetWidth - 4)) + 'px';
  m.style.top = (r.bottom + 4) + 'px';
}
// Page de Réglages d'un document global (config.conf, env.yaml, hub) : lecture seule si l'écriture est refusée.
// Contrôles marqués (secrets, known_hosts, webUI…) : grisés à leur création par r1Gate. Observateur sur les ajouts
// seulement (pas les attributs : pas de boucle).
function r1Lock(root) {
  if (!whoAccess || !root || typeof ST === 'undefined' || !ST.on || typeof docOfPage !== 'function') return;
  const id = docOfPage(); if (!id) return;
  const kind = DOCS[id].kind;
  if (!r1Denied('configSave', kind) && !r1Denied('configApply', kind)) return;
  root.querySelectorAll('#stpg button, #stpg input, #stpg select, #stpg textarea, #stfoot button').forEach((x) => {
    if (x.disabled) return;
    x.disabled = true; x.classList.add('r1off'); if (!x.title) x.title = R1_TIP;
  });
  const t = root.querySelector('.cehead h2');
  if (t && !t.querySelector('.ro')) t.append(el('span', 't-yellow ro', ' · lecture seule'));
}
let r1Obs = null;
function r1Apply() {
  const box = document.getElementById('modalbox'); if (!box) return;
  if (r1Obs) { r1Obs.disconnect(); r1Obs = null; }
  const t = box.querySelector('.cehead h2 .ro'); if (t) t.remove();
  if (!whoAccess || !whoAccess.deny.length) return;
  r1Lock(box);
  r1Obs = new MutationObserver(() => { r1Obs.disconnect(); const t2 = box.querySelector('.cehead h2 .ro'); if (t2 && !(typeof ST !== 'undefined' && ST.on && typeof docOfPage === 'function' && docOfPage())) t2.remove(); r1Lock(box); r1Obs.observe(box, { childList: true, subtree: true }); });
  r1Obs.observe(box, { childList: true, subtree: true });
}
// Session expirée : modale bloquante (identité connue) ; terminaux figés, aucune frappe envoyée.
function expiredModal() {
  if (document.getElementById('expired')) return;
  const o = el('div', 'expired'); o.id = 'expired'; o.setAttribute('role', 'alertdialog'); o.setAttribute('aria-modal', 'true');
  const d = el('div', 'expdlg');
  const b = el('button', 'btn pri', 'Se reconnecter'); b.onclick = () => { location.href = document.baseURI; };
  const bt = el('div', 'btns'); bt.appendChild(b);
  d.append(el('b', '', 'Session expirée'), el('p', '', 'La session du launcher a pris fin. Les terminaux sont figés ; les projets continuent dans leurs pods.'), bt);
  o.appendChild(d); document.body.appendChild(o);
  for (const ev of ['keydown', 'keypress', 'keyup', 'paste', 'input']) document.addEventListener(ev, (e) => { if (!e.target.closest || !e.target.closest('#expired')) { e.stopImmediatePropagation(); e.preventDefault(); } }, true);
  b.focus(); renderWho();
}
let sessionIsLost = false; // vrai aussi après un refus d'hôte/origine : on ne réessaie plus
function stopBanner(msg) {
  if (sessionIsLost) return;
  sessionIsLost = true;
  if (msg === SESSION_MSG && whoUser) { expiredModal(); return; } // identité connue (proxy) : modale bloquante
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
// Environnement réel de startFlow : chaque natif est appelé nu (jamais comme méthode d'env : « Illegal invocation »).
function flowEnv(cb) {
  return {
    watch: (f, ms) => setTimeout(f, ms), unwatch: (h) => clearTimeout(h), setTimeout: (f, ms) => setTimeout(f, ms),
    newES: (u) => new EventSource(u),
    race: (p, ms) => Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error('délai dépassé')), ms))]),
    init: () => transport.call('init'), applyInit, cb, banner: connBanner, lost: () => sessionIsLost,
  };
}
function startFlow(env) {
  let delay = 1500, down = false, initMs = 8000; // délai d'init : 8, 16 puis 30 s au plus
  const lose = () => { if (!down) { down = true; env.banner(true); } };
  // preuve que le serveur répond (init réussi, flux ouvert, message reçu) : bandeau retiré, quel que soit le chemin
  const shown = () => { if (down) { down = false; env.banner(false); } };
  // flux réellement établi (onopen, message) : seul signe qui remet le délai de reprise à 1,5 s (sinon 503 répétés = boucle)
  const alive = () => { delay = 1500; shown(); };
  const retry = () => { if (env.lost()) return; lose(); const w = delay; delay = Math.min(delay * 2, 15000); env.setTimeout(reconnect, w); };
  // veille : le serveur envoie un évènement « ping » toutes les 15 s ; rien pendant 40 s (flux à moitié ouvert, sans
  // onerror) : flux déclaré mort, fermé, bandeau et reprise par init.
  const open = () => {
    const es = env.newES('api/events');
    let wd = null, dead = false;
    const arm = () => { env.unwatch(wd); if (!dead) wd = env.watch(() => { if (dead) return; dead = true; es.close(); console.warn('[flux] aucun évènement depuis 40 s : flux fermé'); retry(); }, 40000); };
    arm();
    es.onopen = () => { alive(); arm(); };
    es.onmessage = (m) => { alive(); arm(); try { const e = JSON.parse(m.data); env.cb(e.name, e.data); } catch (err) { console.error(err); } };
    es.onerror = () => { dead = true; env.unwatch(wd); es.close(); retry(); };
  };
  const reconnect = async () => {
    let r;
    try { r = await env.race(env.init(), initMs); } catch (err) { if (env.lost()) return; initMs = Math.min(initMs * 2, 30000); console.warn('[flux] init échoué, nouvel essai :', err && err.message); retry(); return; }
    initMs = 8000; shown();
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
      const res = await fetch('api/call', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ method, args: args || {} }) });
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
      startFlow(flowEnv(cb));
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

// #242 : xterm.js ignore le `transform: scale(k)` d'un ancêtre pour passer de la souris à la cellule (il divise par la
// taille de cellule non réduite) : le clic tombait plus haut dès qu'un terminal était réduit (bandeau d'attente qui
// rétrécit la zone). Les évènements souris reçus par un terminal d'un conteneur réduit sont donc rejoués avec des
// coordonnées ramenées à l'échelle 1, relatives au `.xterm-screen` du terminal visé (origine du transform en 0 0).
function fixScaledMouse(host) {
  const types = ['mousedown', 'mouseup', 'mousemove', 'click', 'dblclick', 'contextmenu'];
  for (const type of types) {
    host.addEventListener(type, (e) => {
      if (e.__scaleFixed || !e.target || !e.target.closest) return;
      const x = e.target.closest('.xterm');
      const scr = x && x.querySelector('.xterm-screen');
      if (!scr || !scr.offsetWidth) return;
      const r = scr.getBoundingClientRect();
      const k = r.width / scr.offsetWidth;
      if (!(k > 0) || Math.abs(k - 1) < 0.001) return;
      e.stopImmediatePropagation();
      const ne = new MouseEvent(type, {
        bubbles: true, cancelable: true, composed: true, view: window,
        clientX: r.left + (e.clientX - r.left) / k, clientY: r.top + (e.clientY - r.top) / k,
        screenX: e.screenX, screenY: e.screenY, button: e.button, buttons: e.buttons, detail: e.detail,
        ctrlKey: e.ctrlKey, shiftKey: e.shiftKey, altKey: e.altKey, metaKey: e.metaKey,
      });
      ne.__scaleFixed = true;
      e.target.dispatchEvent(ne);
    }, true);
  }
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
      // #235 : date de renouvellement colorée par la projection d'épuisement (yellow = orange à l'écran), alerte rouge
      const lvl = { red: 'rst-red', yellow: 'rst-orange', green: 'rst-green' }[r.resetLevel];
      if (lvl && !r.passed) rst.classList.add(lvl);
      if (r.alert && !r.passed) rst.appendChild(el('span', 'rst-alert', ' ' + r.alert));
      if (r.estimate && !r.passed) { const es = el('span', 'rst-est', ' ' + r.estimate); es.title = 'estimation approximative : échantillon court'; rst.appendChild(es); } // #268 : gris, jamais de couleur d'alerte
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

// ── Utilisateur sans backend (#255, M1) : espace vide, ni projet ni terminal, invitation à ajouter un backend.
// Source : `space.empty` de la réponse `init` (le serveur reste seul juge).
let whoSpace = null; // `space` de init/clientInfo : { admin, login, backends[], empty, error? } (absent pour l'admin)
const noBackend = () => !!(whoSpace && whoSpace.empty === true);
function noBackendBox() {
  const d = el('div', 'nobackend');
  d.append(el('h2', '', 'Ajoute ton premier backend'));
  d.append(el('p', '', 'Tes projets s\'exécutent sur tes propres serveurs (Kubernetes, Docker ou Podman). Le launcher ne stocke rien pour toi : il se connecte à ton backend, y retrouve tes projets et les démarre.'));
  const b = el('button', 'btn primary', '+ Ajouter un backend');
  b.onclick = () => { if (typeof openSettings === 'function') openSettings('', 'backends'); };
  d.append(b);
  const lg = (whoSpace && whoSpace.login) || (whoUser && whoUser.login);
  if (whoSpace && whoSpace.error) d.append(el('p', 't-red', '⚠ ' + String(whoSpace.error)));
  if (lg) d.append(el('p', 'dim', 'Connecté en tant que ' + lg));
  return d;
}

// ── Projets d'un utilisateur (#275, M2 A) : état de la ligne d'après les champs du serveur (arrêté, disparu, injoignable) ──
const fmtDate = (iso) => { const t = Date.parse(iso || ''); return isNaN(t) ? '' : new Date(t).toLocaleString('fr-FR', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' }); };
function fmtAge(iso) {
  const t = Date.parse(iso || ''); if (isNaN(t)) return '';
  const m = Math.max(0, Math.round((Date.now() - t) / 60000));
  if (m < 2) return 'à l\'instant'; if (m < 60) return 'il y a ' + m + ' min'; const h = Math.round(m / 60); if (h < 48) return 'il y a ' + h + ' h'; return 'il y a ' + Math.round(h / 24) + ' j';
}
function projState(r) {
  const gone = r.missing && typeof r.missing === 'object' ? r.missing : null, un = r.unreachable && typeof r.unreachable === 'object' ? r.unreachable : null;
  if (gone) return { kind: 'gone', icon: '✗', tone: 't-red', label: 'introuvable', title: 'introuvable sur ' + (gone.backend || 'le backend') + (gone.seen ? ' (vu le ' + fmtDate(gone.seen) + ')' : '') + ' — Purger efface la trace seulement' };
  if (un) return { kind: 'unreach', icon: '⊘', tone: 't-dim', label: '', title: (un.backend ? 'backend ' + un.backend + ' injoignable' : 'backend injoignable') + (un.since ? ' depuis ' + fmtAge(un.since).replace('il y a ', '') : '') + (un.reason ? ' — ' + un.reason : '') + ' · nom et catégorie seulement ; ouvrir / fermer / F5 désactivés' };
  if (r.stopped) return { kind: 'stopped', icon: '○', tone: 't-dim', label: 'arrêté', title: 'arrêté' + (r.stateAt ? ' · état du ' + fmtDate(r.stateAt) + ' (' + fmtAge(r.stateAt) + ')' : '') };
  return {};
}
// raison d'un projet qu'on ne peut ni ouvrir ni rafraîchir (disparu, injoignable) ; '' : possible
function projBlocked(r) { const st = r ? projState(r) : {}; return st.kind === 'gone' || st.kind === 'unreach' ? st.title : ''; }
function purgeProject(r) {
  const gone = r.missing && typeof r.missing === 'object' ? r.missing : {}, name = (r.path || '').split('/').filter(Boolean).pop() || r.name;
  openModal((b) => {
    b.append(el('h2', '', 'Purger « ' + name + ' » ?'));
    b.append(el('p', '', 'Purger efface seulement la trace de ce projet dans le launcher. Rien n\'est supprimé chez toi : les fichiers éventuels restent dans le stockage du backend' + (gone.backend ? ' « ' + gone.backend + ' »' : '') + '.'));
    const err = el('div', 't-red'); b.appendChild(err);
    const btns = el('div', 'btns');
    const no = el('button', 'btn', 'Annuler'); no.onclick = closeModal;
    const yes = el('button', 'btn danger', 'Purger');
    yes.onclick = async () => {
      err.textContent = ''; yes.disabled = true;
      const segs = (r.path || '').split('/').filter(Boolean);
      let res; try { res = await call('projectPurge', { path: r.path, backend: gone.backend || segs[0] || '', project: segs[segs.length - 1] || '' }); } catch (e) { res = { ok: false, msg: e.message }; }
      yes.disabled = false;
      if (res && res.ok === false) { err.textContent = '✗ ' + (res.msg || 'purge refusée'); return; }
      closeModal(); if (res && res.msg) flash(res.msg);
    };
    btns.append(no, yes); b.appendChild(btns); no.focus();
  });
}

function renderTree() {
  const tree = $('tree'); tree.textContent = '';
  const rows = visibleRows();
  if (!rows.length) tree.appendChild(el('div', 'row empty dim', noBackend() ? 'aucun backend' : 'aucun projet'));
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
      d.append(dot, name);
      // M2 A (#275) : projet arrêté grisé et daté, projet disparu en erreur, backend injoignable ⊘ (raison en infobulle)
      const st = projState(r);
      if (st.kind) {
        d.classList.add(st.kind);
        if (st.icon) { dot.textContent = st.icon; dot.className = 'dot ' + st.tone; }
        if (st.title) { dot.title = st.title; name.title = st.title; }
        if (st.label) d.appendChild(el('span', 'stlabel', st.label));
      }
      if (r.attention) { const ab = el('span', 'attnbadge', '❓ en attente'); ab.title = attnText(r.attention); d.appendChild(ab); }
      d.append(el('span', 'rel', r.release || ''), badgeCells(r));
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
const REFRESH_LABELS = { start: 'démarrage', ctr: 'conteneur / pod', session: 'session', git: 'git', github: 'GitHub', tpl: 'template', usage: 'consommation' };
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
  { const why = projBlocked(r0); if (why) { flash('⚠ ' + why); return; } }
  // bandeau posé avant l'appel : un évènement `done` arrivé avant le retour du call le remplace/l'efface correctement
  const prev = refreshBands[path];
  refreshBands[path] = { name: r0 ? r0.name : '', steps: (r0 && r0.stopped ? ['start'].concat(REFRESH_IDS) : REFRESH_IDS).map((id) => ({ id, state: 'wait' })), done: false, msg: '' }; // projet arrêté : étape « démarrage » en tête
  if (path === S.cursor) renderCards();
  let r;
  try { r = await call('refreshProject', { path }); } catch (e) { r = { ok: false, msg: e.message }; }
  if (r && r.ok === false) {
    if (prev) refreshBands[path] = prev; else delete refreshBands[path];
    if (path === S.cursor) renderCards();
    flash(r.msg || 'rafraîchissement impossible');
  } else if (r && r.ok && Array.isArray(r.steps)) { // projet d'un utilisateur : réponse directe, sans évènements d'étapes
    delete refreshBands[path];
    flash((rowOf(path) || {}).name ? (rowOf(path).name + ' : ' + (r.started ? 'projet démarré, état relu' : 'état relu')) : 'projet rafraîchi', 4000);
    call('select', { path }).then((c) => { delete cardsCache[path]; takeCards(c); }).catch(() => {});
  }
}

// Carte d'un projet d'utilisateur (M2 A) : arrêté = dernier état connu daté (lu sans démarrer), en marche = état du moment.
// Champs : { path, name, backend, found, stopped, image, stateAt, git{branch,dirty,ahead,behind,head}, tpl, team } (jamais de fichier).
function renderUserCard(c, box) {
  const t = el('div', 'cardstitle'); t.append(el('span', '', '◈ ÉTAT · ' + c.name + (c.stopped ? ' · arrêté' : '')));
  box.appendChild(t);
  const rb = refreshBand(c.path); if (rb) box.appendChild(rb);
  const k = (title, body, sub) => { const d = el('div', 'card'); const h = el('div', 'ch'); h.append(el('span', '', title)); if (sub) h.append(el('span', 'dim', ' · ' + sub)); d.append(h, body); box.appendChild(d); };
  const pb = el('div', 'cb');
  if (c.found === false) { pb.appendChild(el('div', 't-red', '✗ introuvable sur ' + (c.backend || 'le backend') + ' — Purger efface la trace seulement')); k('Projet', pb); return; }
  if (c.stopped) {
    const sb = el('div', 'stopbanner');
    sb.append(el('span', 't-orange', '○ arrêté'), el('span', 'dim', c.stateAt ? ' · état du ' + fmtDate(c.stateAt) + ' (' + fmtAge(c.stateAt) + ')' : ' · état inconnu'), el('span', 'sp'));
    const f5 = el('button', 'btn sm pri', 'F5 · démarrer et rafraîchir'); f5.title = 'démarre le projet (quelques secondes) puis relit son état ; il reste en marche';
    f5.onclick = (e) => { e.stopPropagation(); refreshProject(c.path); };
    sb.appendChild(f5); pb.appendChild(sb);
  }
  const kv = el('div', 'kv'), g = c.git || null;
  kv.append(el('span', '', 'backend'), el('span', '', c.backend || ''));
  if (c.image) kv.append(el('span', '', 'image'), el('span', '', c.image));
  if (g) {
    kv.append(el('span', '', 'branche'), el('span', '', g.branch || '—'),
      el('span', '', 'état'), el('span', g.dirty ? 't-yellow' : 't-green', g.dirty ? '✎ ' + (typeof g.dirty === 'number' ? g.dirty + ' fichier(s) modifié(s)' : 'modifié') : 'propre'),
      el('span', '', 'remote'), el('span', '', '↑' + (g.ahead || 0) + ' ↓' + (g.behind || 0)));
    if (g.head) kv.append(el('span', '', 'dernier'), el('span', '', String(g.head)));
  } else kv.append(el('span', '', 'git'), el('span', 'dim', 'pas d\'état connu : F5 pour le lire'));
  if (c.tpl) kv.append(el('span', '', 'template'), el('span', '', String(c.tpl)));
  if (c.team) kv.append(el('span', '', 'team'), el('span', '', String(c.team)));
  pb.appendChild(kv);
  k('Projet', pb);
  const fb = el('div', 'cb'); fb.appendChild(el('div', 'dim', c.stopped ? 'projet arrêté — F5 pour le démarrer' : 'aucun fichier présenté ici'));
  k('Fichiers', fb, c.stopped ? 'projet arrêté' : '');
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
  if (c.found !== undefined) return renderUserCard(c, box); // projet d'un utilisateur : carte lue sur son backend (#275)
  const pend = new Set(c.pending || []);
  const hidden = prefs.get('cardsHidden', false);
  const t = el('div', 'cardstitle');
  const fold = el('span', 'fold' + (hidden ? '' : ' open'), '›');
  fold.title = hidden ? 'afficher les cartes' : 'masquer les cartes';
  t.onclick = (e) => { if (e.target.closest('.cfglink')) return; e.stopPropagation(); setCardsHidden(!hidden); };
  t.append(fold, el('span', '', '◈ ÉTAT · ' + c.name + (c.stopped ? ' · arrêté' : '')));
  const ce = el('span', 'cfglink', '⚙ env.yaml'); ce.title = 'environnement du projet (.claude-launcher/env.yaml)'; ce.onclick = (e) => { e.stopPropagation(); configEditor(c.path, 'project-env'); };
  if (c.updated) t.append(el('span', c.staleUpd ? 't-yellow' : '', '· mis à jour ' + c.updated));
  t.append(el('span', 'sp'), ce);
  box.appendChild(t);
  const rb = refreshBand(c.path); if (rb) box.appendChild(rb);
  if (c.warn) { const w = el('div', 'cardwarn', '⚠ ' + c.warn); w.title = c.warn; box.appendChild(w); } // ancien montage k8s (#253) ; textContent via el() : pas de HTML
  box.classList.toggle('folded', hidden);
  // carte repliable d'un clic sur son bandeau (#124), état mémorisé
  const card = (title, sub, body, eye) => {
    const closed = !!prefs.get('cardsClosed', {})[title];
    const k = el('div', 'card' + (closed ? ' closed' : ''));
    const h = el('div', 'ch');
    h.append(el('span', 'fold' + (closed ? '' : ' open'), '›'), el('span', '', title));
    if (Array.isArray(sub)) { // parties { t, warn } : ⚠ orange après la partie, détail en infobulle (#199)
      const sw = el('span', 'subs'); // tronqué (…) avant l'œil, qui reste à droite (#209)
      const ws = []; // ⚠ « template plus récent » (#199) : hors de la zone tronquée, toujours visible, juste avant l'œil
      sub.forEach((p) => { sw.append(el('span', 'dim', ' · ' + p.t)); if (p.warn) { const w = el('span', 't-orange warn', '⚠'); w.title = p.warn; ws.push(w); } });
      h.append(sw, ...ws);
    } else if (sub) h.append(el('span', 'dim', ' · ' + sub));
    if (eye) h.append(eye);
    h.onclick = (e) => { e.stopPropagation(); const m = Object.assign({}, prefs.get('cardsClosed', {})); if (m[title]) delete m[title]; else m[title] = true; prefs.set('cardsClosed', m); renderCards(); };
    k.append(h);
    if (!closed) k.append(body);
    if (!hidden) box.appendChild(k);
    return k;
  };
  // Projet : légende des icônes de la ligne, puis git et versions
  const pb = el('div', 'cb');
  if (c.stopped) { // projet arrêté (M2 A) : dernier état connu daté, lu sans démarrer ; F5 démarre et relit
    const sb = el('div', 'stopbanner');
    sb.append(el('span', 't-orange', '○ arrêté'), el('span', 'dim', c.stateAt ? ' · état du ' + fmtDate(c.stateAt) + ' (' + fmtAge(c.stateAt) + ')' : ' · état inconnu'), el('span', 'sp'));
    const f5 = el('button', 'btn sm pri', 'F5 · démarrer et rafraîchir'); f5.title = 'démarre le projet (5 à 30 s) puis relit son état ; il reste en marche';
    f5.onclick = (e) => { e.stopPropagation(); refreshProject(c.path); };
    sb.appendChild(f5); pb.appendChild(sb);
  }
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
  const pk = card('Projet', '', pb);
  if (pk && pk.firstChild && c.path) { // #216 : menu ⋯ de la carte (mêmes actions que le clic droit de l'arbre)
    const mb = el('span', 'cardmenu', '⋯'); mb.title = 'Actions du projet';
    mb.onclick = (e) => { e.stopPropagation(); const r = mb.getBoundingClientRect(); showProjectMenu(c.path, r.left, r.bottom); };
    pk.firstChild.append(mb);
  }
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
      r.append(mark, el('span', 'n', (m.companion ? '+ ' : '') + m.name), teamCell(c, m), warn);
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
  // plus de puce « masquage N s » (#209 v2) : l'état est porté par l'œil, dernier élément du titre
  const hi = th.hideIdle;
  card('Team', sub, tb, hi && hi.mode ? hideEye(c.path, hi) : null);
  // Fichiers
  if (c.loaded) {
    const fb = el('div', 'cb'); const g = el('div', 'files');
    const mark = (ch) => (!ch ? ['', ''] : ch === 'D' ? ['t-red', ch] : (ch === '?' || ch === 'A') ? ['t-green', ch] : ['t-yellow', ch]);
    for (const f of c.files || []) {
      const [cls, ch] = mark(f.change);
      const n = el('span', 'n' + (f.dir ? ' dir' : ''), f.name + (f.dir ? '/' : '')); n.title = f.name;
      g.append(el('span', cls, ch), n, el('span', 'dim', f.meta));
    }
    if (c.stopped) g.append(el('span'), el('span', 'dim', 'projet arrêté — F5 pour le démarrer'), el('span')); // jamais une erreur muette
    else if (!c.files || !c.files.length) g.append(el('span'), el('span', 'dim', '(vide)'), el('span'));
    fb.appendChild(g);
    card('Fichiers', c.stopped ? 'projet arrêté' : String((c.files || []).length), fb);
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
  hideOverlay(c);
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
  { const why = projBlocked(rowOf(path)); if (why) { flash('⚠ ' + why); return; } } // disparu / injoignable : la raison, jamais un échec muet
  const r = await call('open', { path });
  if (!r) return;
  if (r.execChoice) return execChoice(r.execChoice, true);
  if (r.needTrust) return envTrustBox(r.path || path); // #215 : environnement d'un dépôt cloné, à approuver
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
// ── Œil 3 états du titre de la carte TEAM (#209) : masquage des inactifs propre au projet ─────────────────────
// ouvert = no hide, fermé = auto hide, grisé = default (suit le global #188, dessin = effet du global).
// Clic : default → auto → none → default ; clic droit : menu des 3 états ; écriture par projectApply {hideIdle}.
const EYE_OPEN = '<svg viewBox="0 0 24 16" width="18" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 8s4-7 11-7 11 7 11 7-4 7-11 7S1 8 1 8z"/><circle cx="12" cy="8" r="3"/></svg>';
const EYE_SHUT = '<svg viewBox="0 0 24 16" width="18" height="14" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M1 6s4 6 11 6 11-6 11-6"/><path d="M5 10l-2 3M12 12v3M19 10l2 3"/></svg>';
// Fonctions pures (autonomes : extraites telles quelles par testdata/hide_eye.js) — g = { enabled, delay } = réglage global
function hideEyeNext(mode) { return mode === 'default' ? 'auto' : mode === 'auto' ? 'none' : 'default'; }
function hideEyeView(mode, g) {
  const name = mode === 'auto' ? 'auto hide' : mode === 'none' ? 'no hide' : 'default';
  const nx = hideEyeNext(mode), nxName = nx === 'auto' ? 'auto hide' : nx === 'none' ? 'no hide' : 'default';
  const open = mode === 'none' || (mode !== 'auto' && !g.enabled);
  return { mode, open, dim: mode === 'default', own: mode === 'auto' || mode === 'none', name,
    tip: 'Ce projet : ' + name + ' — clic : ' + nxName + ' · réglage global : ' + (g.enabled ? 'masqués après ' + g.delay + ' s' : 'désactivé') };
}
function hideEyeMenu(mode, g) {
  return [{ mode: 'auto', label: 'auto hide — masquer après ' + g.delay + ' s' },
    { mode: 'default', label: 'default — réglage global (' + (g.enabled ? 'masqués après ' + g.delay + ' s' : 'désactivé') + ')' },
    { mode: 'none', label: 'no hide — toujours visibles' }].map((e) => ({ mode: e.mode, label: e.label, current: e.mode === mode }));
}
function hideEyeApply(mode) { return { hideIdle: mode }; }
const hideBusy = {}; // chemin -> true pendant l'écriture (pas de second clic)
const hideG = (hi) => ({ enabled: !!hi.global, delay: hi.delay });
const hideIcon = (v) => (v.open ? EYE_OPEN : EYE_SHUT);
function hideEye(path, hi) {
  const v = hideEyeView(hi.mode, hideG(hi));
  const e = el('span', 'eye st-' + hi.mode + (v.own ? ' own' : ''));
  e.innerHTML = hideIcon(v);
  e.title = v.tip; e.tabIndex = 0; e.setAttribute('role', 'button');
  e.setAttribute('aria-label', e.title);
  e.onclick = (ev) => { ev.stopPropagation(); setHideIdle(path, hideEyeNext(hi.mode), e); };
  e.onkeydown = (ev) => {
    if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); ev.stopPropagation(); setHideIdle(path, hideEyeNext(hi.mode), e); }
    else if (ev.key === 'ContextMenu' || (ev.shiftKey && ev.key === 'F10')) { ev.preventDefault(); ev.stopPropagation(); const r = e.getBoundingClientRect(); showHideMenu(path, hi, r.left, r.bottom); }
  };
  e.addEventListener('contextmenu', (ev) => { ev.preventDefault(); ev.stopPropagation(); showHideMenu(path, hi, ev.clientX, ev.clientY); });
  e.addEventListener('mousedown', (ev) => ev.stopPropagation());
  if (hideBusy[path]) e.classList.add('busy');
  return e;
}
async function setHideIdle(path, mode, eyeEl) {
  if (hideBusy[path]) return;
  hideBusy[path] = true; if (eyeEl) eyeEl.classList.add('busy');
  let r;
  try { r = await call('projectApply', Object.assign({ path }, hideEyeApply(mode))); } catch (e) { r = null; }
  delete hideBusy[path];
  if (!r || r.ok === false) { flash('⚠ ' + ((r && r.msg) || 'réglage du masquage non enregistré')); renderCards(); return; }
  flashBrief('Teammates inactifs : ' + hideEyeView(mode, { enabled: true, delay: 0 }).name, 3000);
  // affichage immédiat, maintenu seulement jusqu'à la relecture qui suit l'appel (le cœur applique la visibilité au retour)
  hideOver[path] = { mode, until: Date.now() + 5000 }; // garde de secours ; levée dès la relecture ci-dessous
  if (cardsCache[path]) { cardsCache[path] = hideOverlay(Object.assign({}, cardsCache[path])); if (path === S.cursor) { S.cards = cardsCache[path]; renderCards(); } }
  const done = () => { delete hideOver[path]; };
  call('cardsFor', { path }).then((c) => { takeCards(c); done(); }).catch(done);
}
const hideOver = {}; // chemin -> { mode, until } : choix à afficher tant que la carte reçue ne le porte pas encore
function hideOverlay(c) {
  const o = hideOver[c.path], hi = c.teamHead && c.teamHead.hideIdle;
  if (!o || !hi) return c;
  if (hi.mode === o.mode || Date.now() > o.until) { delete hideOver[c.path]; return c; }
  c.teamHead = Object.assign({}, c.teamHead, { hideIdle: Object.assign({}, hi, { mode: o.mode, enabled: (o.mode === 'auto' || (o.mode === 'default' && hi.global)) && hi.delay > 0 }) });
  return c;
}
function showHideMenu(path, hi, x, y) {
  closeCtx();
  const m = el('div', 'ctxmenu'); m.id = 'ctxmenu';
  m.appendChild(el('div', 'dis hd', 'Teammates inactifs — ce projet'));
  const g = hideG(hi);
  for (const o of hideEyeMenu(hi.mode, g)) {
    const d = el('div', o.current ? 'sel' : '');
    const ov = hideEyeView(o.mode, g);
    const i = el('span', 'mi' + (o.mode === 'default' ? ' st-default' : '') + (ov.own ? ' own' : '')); i.innerHTML = hideIcon(ov);
    d.append(i, el('span', '', o.label));
    d.onclick = (ev) => { ev.stopPropagation(); closeCtx(); if (!o.current) setHideIdle(path, o.mode, null); };
    m.appendChild(d);
  }
  document.body.appendChild(m);
  const w = m.offsetWidth, h = m.offsetHeight;
  m.style.left = Math.max(0, Math.min(x, window.innerWidth - w - 4)) + 'px';
  m.style.top = Math.max(0, Math.min(y, window.innerHeight - h - 4)) + 'px';
}

// ── Modèle et effort d'un teammate depuis la carte TEAM (#250, maquette M1 A « Menu sur modèle · effort ») ──
// Fonctions pures (autonomes : extraites telles quelles par un test node). m = ligne de carte (TeamRow), table = teamHead.modelEfforts.
const TEAM_MODELS = ['opus', 'sonnet', 'haiku', 'fable'], TEAM_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];
const TEAM_REMOTE_TIP = 'réglage réservé à la fenêtre native';
function teamEffortsFor(table, model) {
  const t = table || {};
  return Array.isArray(t[model]) ? t[model] : Array.isArray(t['*']) ? t['*'] : TEAM_EFFORTS;
}
function teamSrcLabel(src, file) { return src === 'companion' ? 'compagnon' + (file ? ' + ' + file : '') : src === 'template' ? 'template' : '?'; }
// cellule « modèle · effort » : texte, ⟳, état cliquable, infobulle
function teamCellView(m, remote) {
  const file = m.companionFile || (m.agent ? m.agent + '.md' : '');
  const run = m.running || null;
  const nx = !!(run && (run.model || run.effort));
  const text = (m.model || '—') + ' · ' + (m.effort || '—') + (nx ? ' ⟳' : '');
  const lines = [];
  let clickable = false;
  if (remote) lines.push(TEAM_REMOTE_TIP);
  else if (m.editable !== true) lines.push('non réglable : ' + (m.agent ? 'pas de modèle ni d\'effort dans le template de cet agent' : 'agent lancé hors registre'));
  else {
    clickable = true;
    lines.push('Modèle : ' + (m.model || '—') + ' (' + teamSrcLabel(m.modelSrc, m.modelSrc === 'companion' ? file : '') + ') · Effort : ' + (m.effort || '—') + ' (' + teamSrcLabel(m.effortSrc, m.effortSrc === 'companion' ? file : '') + ') — clic : changer');
  }
  if (nx) lines.push('En cours : ' + (run.model || m.model || '—') + ' · ' + (run.effort || 'effort inconnu'), '⟳ ' + (m.model || '—') + ' · ' + (m.effort || '—') + ' au prochain spawn');
  return { text, nx, clickable, locked: !clickable, tip: lines.join('\n') };
}
// contenu du menu : entrées Modèle et Effort (« Défaut (template) » en tête, jamais grisée), note de bas de menu
function teamMenuView(m, table) {
  const entry = (kind, value, cur, src, tpl, disabled, tip) => ({ kind, value, current: !!cur, tag: value !== 'default' && value === tpl ? 'template = défaut' : '', disabled: !!disabled, tip: tip || '',
    label: value === 'default' ? 'Défaut (template) — ' + (tpl || '—') : value });
  const models = [entry('model', 'default', m.modelSrc === 'template', m.modelSrc, m.tplModel)]
    .concat(TEAM_MODELS.map((v) => entry('model', v, m.modelSrc === 'companion' && m.model === v, m.modelSrc, m.tplModel)));
  const ok = teamEffortsFor(table, m.model);
  const efforts = [entry('effort', 'default', m.effortSrc === 'template', m.effortSrc, m.tplEffort)]
    .concat(TEAM_EFFORTS.map((v) => {
      const bad = ok.indexOf(v) < 0;
      return entry('effort', v, m.effortSrc === 'companion' && m.effort === v, m.effortSrc, m.tplEffort, bad, bad ? 'non pris en charge par ' + (m.model || 'ce modèle') : '');
    }));
  const file = m.companionFile || (m.agent ? m.agent + '.md' : '');
  const lead = m.agent === 'teamleader';
  const foot = ['Modèle : ' + (m.model || '—') + ' (' + teamSrcLabel(m.modelSrc, m.modelSrc === 'companion' ? file : '') + ') · Effort : ' + (m.effort || '—') + ' (' + teamSrcLabel(m.effortSrc, m.effortSrc === 'companion' ? file : '') + ')',
    'Écrit dans .claude/agents/' + (file || '<agent>.md') + (lead ? ' et .claude/settings.json (model, effortLevel)' : '') + ' · appliqué au prochain spawn'];
  const warn = lead ? '⚠ La session en cours garde ' + (m.model || '—') + ' · ' + (m.effort || '—') + ' : /model, /effort pour la changer maintenant. La prochaine session suit settings.json.' : '';
  return { title: 'Modèle — ' + m.name, models, efforts, all: { label: 'Tout par défaut (template : ' + (m.tplModel || '—') + ' · ' + (m.tplEffort || '—') + ')' }, foot, warn };
}
// argument de teamAgentSet ; what : 'model' | 'effort' | 'all'
function teamSetArgs(path, m, what, value) {
  const a = { path, agent: m.agent };
  if (what === 'all') { a.model = 'default'; a.effort = 'default'; } else a[what] = value;
  return a;
}
// message de retour : { warn, text }
function teamSetFlash(res, m) {
  if (!res) return { warn: true, text: 'réglage du modèle non enregistré' };
  if (res.ok === false) {
    const base = res.msg || (res.remote ? TEAM_REMOTE_TIP : res.timeout ? 'partage injoignable' : 'refusé');
    return { warn: true, text: res.partial === 'companion' ? base + ' (compagnon écrit, settings.json à réaligner)' : base };
  }
  let t = m.name + ' : ' + (res.model || m.model) + ' · ' + (res.effort || m.effort) + (res.removed ? ' — compagnon supprimé (template)' : ' enregistré' + (res.file ? ' (' + res.file + ')' : ''));
  if (res.effortAdjusted) t += ' — effort ajusté au modèle';
  if (res.note) t += ' — ' + res.note;
  return { warn: false, text: t };
}

let clientRemote = false;   // clientInfo : requête web hors boucle locale (P1) ; le serveur reste seul juge
const teamBusy = {};        // chemin|agent -> true pendant l'écriture (pas de second clic)
function teamCell(c, m) {
  const v = teamCellView(m, clientRemote);
  const e = el('span', 'm' + (v.clickable ? ' hot' : '') + (v.locked && m.editable === true ? ' locked' : ''), v.text);
  if (v.nx) { e.textContent = ''; e.append(v.text.slice(0, -2), el('span', 'nx', ' ⟳')); }
  e.title = v.tip;
  if (!v.clickable) return e;
  e.tabIndex = 0; e.setAttribute('role', 'button'); e.setAttribute('aria-label', v.tip.split('\n')[0]);
  if (teamBusy[c.path + '|' + m.agent]) e.classList.add('busy');
  const open = (x, y) => showTeamMenu(c, m, x, y);
  e.onclick = (ev) => { ev.stopPropagation(); const r = e.getBoundingClientRect(); open(r.left, r.bottom); };
  e.onkeydown = (ev) => {
    if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); ev.stopPropagation(); const r = e.getBoundingClientRect(); open(r.left, r.bottom); }
  };
  e.addEventListener('mousedown', (ev) => ev.stopPropagation());
  return e;
}
async function setTeamAgent(c, m, what, value) {
  const k = c.path + '|' + m.agent;
  if (teamBusy[k]) return;
  teamBusy[k] = true; renderCards();
  let r = null;
  try { r = await transport.call('teamAgentSet', teamSetArgs(c.path, m, what, value)); } catch (err) { r = { ok: false, msg: err && err.message }; }
  delete teamBusy[k];
  const f = teamSetFlash(r, m);
  if (f.warn) flash('⚠ ' + f.text); else flashBrief(f.text, 6000);
  renderCards();
  call('cardsFor', { path: c.path }).then(takeCards).catch(() => {});
}
function showTeamMenu(c, m, x, y) {
  closeCtx();
  const v = teamMenuView(m, c.teamHead && c.teamHead.modelEfforts);
  const menu = el('div', 'ctxmenu teammenu'); menu.id = 'ctxmenu';
  const item = (o, what, value) => {
    const d = el('div', (o.current ? 'sel' : '') + (o.disabled ? ' dis' : ''));
    d.append(el('span', 'ck', o.current ? '✓' : ''), el('span', '', o.label));
    if (o.tag) d.appendChild(el('span', 'tag', o.tag));
    if (o.tip) d.title = o.tip;
    if (!o.disabled) d.onclick = (ev) => { ev.stopPropagation(); closeCtx(); if (!o.current) setTeamAgent(c, m, what, value); };
    menu.appendChild(d);
  };
  menu.appendChild(el('div', 'dis hd', v.title));
  for (const o of v.models) item(o, 'model', o.value);
  menu.appendChild(el('hr'));
  menu.appendChild(el('div', 'dis hd', 'Effort'));
  for (const o of v.efforts) item(o, 'effort', o.value);
  menu.appendChild(el('hr'));
  const all = el('div'); all.append(el('span', 'ck', '↺'), el('span', '', v.all.label));
  all.onclick = (ev) => { ev.stopPropagation(); closeCtx(); setTeamAgent(c, m, 'all'); };
  menu.append(all, el('hr'));
  const ft = el('div', 'ft'); ft.append(v.foot[0], el('br'), v.foot[1]);
  if (v.warn) { ft.appendChild(el('br')); ft.appendChild(el('span', 't-yellow', v.warn)); }
  menu.appendChild(ft);
  document.body.appendChild(menu);
  const w = menu.offsetWidth, h = menu.offsetHeight;
  menu.style.left = Math.max(0, Math.min(x, window.innerWidth - w - 4)) + 'px';
  menu.style.top = Math.max(0, Math.min(y, window.innerHeight - h - 4)) + 'px';
}
// P1 : une fois au démarrage ; échec (serveur plus ancien) = non distant
transport.call('clientInfo', {}).then((r) => { clientRemote = !!(r && r.remote); r1Info(r); if (S.cards) renderCards(); }).catch(() => {});

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
    return d;
  };
  { // projet disparu ou backend injoignable : seules actions possibles, avec la raison
    const why = projBlocked(r);
    if (why) {
      m.appendChild(Object.assign(el('div', 'dis hd', why)));
      it('Ouvrir', 'Entrée', () => {}, true); it('⟳ Rafraîchir', 'F5', () => {}, true);
      m.appendChild(el('hr'));
      it('Purger…', '', () => purgeProject(r));
      document.body.appendChild(m);
      m.style.left = Math.max(0, Math.min(x, window.innerWidth - m.offsetWidth - 4)) + 'px'; m.style.top = Math.max(0, Math.min(y, window.innerHeight - m.offsetHeight - 4)) + 'px';
      return;
    }
  }
  it('Ouvrir', 'Entrée', () => openProject(path));
  // Fermer : selon l'état de la ligne (ouvert ou pastille autre que ○), plus selon la présence d'un onglet (#158)
  it('Fermer', 'Suppr', () => askClose(path), !(r.open || (r.dot && r.dot.icon && r.dot.icon !== '○')));
  it(r.stopped ? '⟳ F5 · démarrer et rafraîchir' : '⟳ Rafraîchir', 'F5', () => refreshProject(path), !!r.refreshing);
  it('Forcer la fermeture…', '', () => askForceClose(path), !r.canForce);
  m.appendChild(el('hr'));
  const mv = moveMenuState(r); // #216 : projet ouvert = grisé (les autres blocages restent dans le dialogue)
  const mi = it('⇄ Déplacer / renommer…', '', () => moveBox(path), mv.disabled);
  if (mv.why) mi.title = mv.why;
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
$('login').addEventListener('click', () => r1Denied('login') ? flash('⚠ ' + R1_TIP) : call('login').then(() => {}).catch(() => {}));
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
// Onglets de projets (#189, #190, #191) — fonctions pures (testées par node : testdata/tabs_bar.js).
// tabView : titre = nom seul, toujours ; couleur du projet même hors sélection ; « agent au travail » en infobulle.
// #223 : le teamleader attend une réponse (champ `attention` du contrat : kind question|permission, text, id)
function attnText(a) { return (a.kind === 'permission' ? 'permission : ' : 'question : ') + (a.text || ''); }
function attnAny(s) { return ((s && s.tabs) || []).some((t) => t.attention) || ((s && s.rows) || []).some((r) => r.attention); }
function tabView(t, on) {
  const a = t.attention;
  return { title: (a ? '❓ ' : '') + t.name, tip: a ? 'en attente — ' + attnText(a) : (t.busy ? 'agent au travail' : ''), gear: !!t.busy, color: t.color || '#1F4FD1', cls: (on ? ['on'] : []).concat(t.busy ? ['busy'] : [], a ? ['attn'] : []) };
}
// tabOrder : ordre enregistré d'abord ; les chemins nouveaux à la fin (ordre d'origine) ; les disparus ignorés.
function tabOrder(paths, saved) {
  const known = (saved || []).filter((p) => paths.includes(p));
  return known.concat(paths.filter((p) => !known.includes(p)));
}
// tabMove : `path` à l'indice final `index` (borné) ; Teamleaders jamais déplaçable ; chemin inconnu ignoré.
function tabMove(order, path, index) {
  const from = order.indexOf(path);
  if (from < 0 || path === 'leaders') return order.slice();
  const out = order.filter((p) => p !== path);
  out.splice(Math.max(0, Math.min(out.length, index)), 0, path);
  return out;
}
function tabOrderLoad(store) {
  try { const v = JSON.parse(store.getItem('cl.tabOrder')); return Array.isArray(v) ? v.filter((p) => typeof p === 'string') : []; } catch (e) { return []; }
}
function tabOrderSave(store, order) { try { if (!store) return; store.setItem('cl.tabOrder', JSON.stringify(order)); } catch (e) { /* stockage indisponible : ordre non conservé */ } }
function tabCycleTarget(ids, cur, d) { return ids[(Math.max(0, ids.indexOf(cur)) + d + ids.length) % ids.length]; }
// dropIndex : point d'insertion (0..n) d'après l'abscisse x et les milieux des onglets, dans l'ordre affiché.
function dropIndex(mids, x) { let i = 0; while (i < mids.length && x > mids[i]) i++; return i; }
// tabDragStep : machine du glisser. ev : down{path,x,onClose} | move{x} | up | key{key} ; action : start | drop | cancel | click.
function tabDragStep(state, ev) {
  const st = state || { phase: 'idle' };
  switch (ev.type) {
    case 'down':
      if (ev.onClose) return { state: { phase: 'close' }, action: null };            // × ne lance jamais de glisser
      if (ev.path === 'leaders') return { state: { phase: 'fixed' }, action: null };  // Teamleaders reste fixe
      return { state: { phase: 'down', x: ev.x }, action: null };
    case 'move':
      if (st.phase === 'down' && Math.abs(ev.x - st.x) >= 5) return { state: { phase: 'drag' }, action: 'start' };
      return { state: st, action: null };
    case 'up':
      if (st.phase === 'drag') return { state: { phase: 'idle' }, action: 'drop' };
      if (st.phase === 'down' || st.phase === 'fixed') return { state: { phase: 'idle' }, action: 'click' };
      return { state: { phase: 'idle' }, action: null };
    case 'key':
      if (ev.key === 'Escape' && st.phase === 'drag') return { state: { phase: 'cancelled' }, action: 'cancel' };
      return { state: st, action: null };
  }
  return { state: st, action: null };
}
function savedTabOrder() { try { return tabOrderLoad(localStorage); } catch (e) { return []; } }
// Onglets dans l'ordre affiché (ordre enregistré par poste, nouveaux à la fin).
function displayedTabs() {
  const tabs = (S.snap && S.snap.tabs) || [];
  const by = new Map(tabs.map((t) => [t.path, t]));
  return tabOrder(tabs.map((t) => t.path), savedTabOrder()).map((p) => by.get(p));
}
// Glisser-déposer : après 5 px, trait bleu au point de dépôt, Échap annule, × ne lance jamais de glisser ; pointercancel et
// perte du focus de la fenêtre annulent proprement ; la barre défile seule près de ses bords si elle déborde.
function startTabDrag(ev, path) {
  if (ev.button !== 0) return;
  let ds = tabDragStep({ phase: 'idle' }, { type: 'down', path, x: ev.clientX, onClose: !!ev.target.closest('.x') }).state;
  let at = 0, ind = null, lastX = ev.clientX, speed = 0, iv = null; const tabEl = ev.currentTarget, bar = $('tabs').querySelector('.tscroll');
  const tabsEls = () => [...bar.querySelectorAll('.tab')];
  const place = () => {
    const els = tabsEls(), box = bar.getBoundingClientRect();
    at = dropIndex(els.map((t) => t.offsetLeft + t.offsetWidth / 2 - bar.scrollLeft + box.left), lastX);
    const ref = els[Math.min(at, els.length - 1)];
    ind.style.left = (at >= els.length ? ref.offsetLeft + ref.offsetWidth : ref.offsetLeft) - 2 + 'px';
  };
  const finish = (r) => {
    document.removeEventListener('pointermove', move, true); document.removeEventListener('pointerup', up, true); document.removeEventListener('keydown', key, true);
    document.removeEventListener('pointercancel', abort, true); window.removeEventListener('blur', abort);
    clearInterval(iv);
    if (ind) ind.remove();
    if (r === 'drop' || r === 'cancel') {
      tabEl.classList.remove('ghost');
      if (r === 'drop') {
        const cur = displayedTabs().map((t) => t.path), from = cur.indexOf(path);
        tabOrderSave(localStorageOrNull(), tabMove(cur, path, at > from ? at - 1 : at));
      }
      S.noClick = true; setTimeout(() => { S.noClick = false; }, 0); // le clic qui suit le dépôt ne sélectionne pas
      S.drag = null; renderTabs();
    }
  };
  const step = (e) => { const r = tabDragStep(ds, e); ds = r.state; return r.action; };
  const move = (e) => {
    lastX = e.clientX;
    const a = step({ type: 'move', x: e.clientX });
    if (a === 'start') {
      S.drag = { path }; tabEl.classList.add('ghost'); ind = el('div', 'tdrop'); bar.appendChild(ind);
      iv = setInterval(() => { if (speed) { bar.scrollLeft += speed; place(); } }, 16); // défilement près des bords
    }
    if (!S.drag) return;
    const box = bar.getBoundingClientRect(), edge = 36;
    speed = bar.scrollWidth <= bar.clientWidth ? 0 : e.clientX < box.left + edge ? -Math.ceil((box.left + edge - e.clientX) / 4) : e.clientX > box.right - edge ? Math.ceil((e.clientX - (box.right - edge)) / 4) : 0;
    place();
  };
  const up = () => { const a = step({ type: 'up' }); finish(a === 'drop' ? 'drop' : 'end'); };
  const key = (e) => { if (e.key === 'Escape' && step({ type: 'key', key: 'Escape' }) === 'cancel') { e.preventDefault(); e.stopPropagation(); finish('cancel'); } };
  const abort = () => { finish(S.drag ? 'cancel' : 'end'); }; // pointercancel, fenêtre qui perd le focus
  document.addEventListener('pointermove', move, true); document.addEventListener('pointerup', up, true); document.addEventListener('keydown', key, true);
  document.addEventListener('pointercancel', abort, true); window.addEventListener('blur', abort);
}
function localStorageOrNull() { try { return localStorage; } catch (e) { return null; } }
function renderTabs() {
  if (S.drag) return; // glisser en cours : l'affichage est rafraîchi à la fin du geste
  const bar = $('tabs'); bar.textContent = '';
  // Teamleaders (#162) : pastille à contour épinglée à gauche, hors de la zone qui défile, suivie d'un filet, sans ×
  const lt = el('div', 'tl' + (S.view === 'leaders' ? ' on' : '') + (attnAny(S.snap) ? ' attn' : ''));
  if (attnAny(S.snap)) lt.title = 'Un teamleader attend une réponse';
  lt.append(el('span', 'tlic', '⌗'), el('span', 'tltx', ' Teamleaders'));
  lt.onclick = () => showView('leaders');
  const scroll = el('div', 'tscroll');
  bar.append(lt, el('div', 'tsep'), scroll, layoutButton());
  for (const t of displayedTabs()) {
    const d = el('div', 'tab'); d.dataset.path = t.path;
    // #189 : couleur du projet en variable CSS (liseré bas hors sélection, fond plein sélectionné) ;
    // #190 : titre = nom seul, emplacement d'état réservé (largeur fixe) — rien ne change à la sélection
    const tv = tabView(t, S.view === t.path);
    d.style.setProperty('--c', tv.color);
    for (const c of tv.cls) d.classList.add(c);
    if (tv.tip) d.title = tv.tip;
    d.append(el('span', '', tv.title), el('span', 'slot'));
    if (t.state === 'opening') d.append(el('span', 'st', '⏳'));
    if (t.state === 'error') d.append(el('span', 'st t-red', '⚠'));
    if (t.readOnly) d.append(el('span', 'st', '👁'));
    const x = el('span', 'x', '×'); x.title = 'détacher (la session continue dans tmux)';
    x.onclick = (e) => { e.stopPropagation(); detachTab(t.path); };
    d.appendChild(x);
    d.onclick = () => { if (S.noClick) return; showView(t.path, true); };
    d.addEventListener('pointerdown', (e) => startTabDrag(e, t.path));
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
function cycleTab(d) {
  const tabs = displayedTabs(); // ordre affiché (#191)
  const ids = ['leaders'].concat(tabs.map((t) => t.path));
  showView(tabCycleTarget(ids, S.view, d), true); // (i + d + ids.length) % ids.length : circulaire
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
    fixScaledMouse(this.panesEl); // #242
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
      th.append(el('span', '', tab.name), el('span', 'dim', '· team-lead'), el('span', 'attnbadge tilebadge hidden', '❓ en attente'));
      const qb = el('div', 'qbar hidden');
      th.style.background = tab.color;
      fixScaledMouse(cl); // #242
      body.appendChild(cl); tile.append(th, qb, body);
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
      t = this.tiles[path] = { tile, body, cl, qb, term, pane: l.leader, w: lp.w, h: lp.h };
      call('termSync', { path }); // contenu actuel du leader (capture)
      this.layout();
    }
    this.attn(path);
    if (t.w !== lp.w || t.h !== lp.h) { t.w = lp.w; t.h = lp.h; t.term.resize(lp.w, lp.h); this.scale(t); }
    t.term.options.theme = Object.assign({}, t.term.options.theme, { background: lp.bg });
  },
  // #223 : cadre orange, pastille dans le titre et bandeau de la question ; retirés dès que l'attente cesse
  attn(path) {
    const t = this.tiles[path]; if (!t) return;
    const tab = ((S.snap && S.snap.tabs) || []).find((x) => x.path === path);
    const a = tab && tab.attention;
    t.tile.classList.toggle('attn', !!a);
    t.tile.querySelector('.tilebadge').classList.toggle('hidden', !a);
    t.qb.classList.toggle('hidden', !a);
    t.qb.textContent = a ? '❓ ' + attnText(a) : '';
    t.qb.title = a ? attnText(a) : '';
    this.scale(t);
  },
  attnAll() { for (const p of Object.keys(this.tiles)) this.attn(p); },
  out(path, c) { const t = this.tiles[path]; if (t && c.pane === t.pane) { if (c.reset && !c.keep) t.term.reset(); if (c.data) t.term.write(c.data); } },
  drop(path) { const t = this.tiles[path]; if (!t) return; t.term.dispose(); t.tile.remove(); delete this.tiles[path]; this.layout(); },
  layout() {
    const n = Object.keys(this.tiles).length;
    const g = this.grid;
    g.querySelectorAll('.empty').forEach((e) => e.remove());
    if (!n) {
      const e = el('div', 'empty');
      if (noBackend()) e.append(noBackendBox());
      else e.innerHTML = 'Aucun projet ouvert.<br>Entrée ou double-clic sur un projet pour l\'ouvrir ; ses teamleaders apparaîtront ici.';
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
  r1Apply();
  const f = box.querySelector('input, select, .btn.pri, .btn');
  if (f) f.focus();
}
function closeModal() { if (typeof stKeepClear === 'function') stKeepClear(); if (typeof ST !== 'undefined') { ST.on = false; clearTimeout(ST.timer); } $('modal').classList.add('hidden'); S.newDlg = null; if (typeof eyesHide === 'function') eyesHide(); $('nav').focus(); }
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

// ── Nouveau projet (#214) : base, catégorie, nom, chemin final ───────────────
// Fonctions pures (testées par node, testdata/new_project.js).
// Pré-sélection depuis un dossier de l'arbre : la base la plus longue qui le contient, le reste = catégorie.
function splitPreselect(bases, path) {
  const norm = (x) => String(x || '').replace(/\\/g, '/').replace(/\/+$/, '');
  const p = norm(path); let best = null;
  for (const b of bases || []) {
    const bp = norm(b);
    if ((p === bp || p.startsWith(bp + '/')) && (!best || bp.length > norm(best).length)) best = b;
  }
  if (!best) return { base: '', category: '' };
  return { base: best, category: p.slice(norm(best).length).replace(/^\//, '') };
}
const joinNewPath = (base, category, name) => {
  const sep = String(base).includes('\\') && !String(base).includes('/') ? '\\' : '/';
  return [String(base).replace(/[\\/]+$/, ''), category, name].filter((x) => x).join(sep);
};
// Chargeur de catégories « le dernier appel gagne » : un retard de réponse d'une ancienne base est ignoré.
function makeCatLoader(fetchCats, apply) {
  let seq = 0;
  return async (base) => {
    const my = ++seq;
    let r = null;
    try { r = await fetchCats(base); } catch (e) { r = null; }
    if (my !== seq) return false;
    apply(base, (r && r.categories) || []);
    return true;
  };
}
const NEWCAT = '\u0000new';
// « nouvelle catégorie » choisie sans nom : refus (sinon le projet serait créé à la racine de la base).
const categoryError = (isNew, value) => (isNew && !String(value || '').trim() ? 'nom de catégorie requis' : '');

// Dépôt Git (#215). Fonctions pures testées par node (testdata/new_git.js).
// 'https' | 'ssh' | 'bad' (file://, chemin local, option -…, schéma inconnu) | '' (vide).
function gitUrlKind(u) {
  u = String(u || '').trim(); if (!u) return '';
  if (/^-/.test(u) || /\s/.test(u)) return 'bad';
  if (/^https:\/\/[^/\s]+\/\S+/i.test(u)) return 'https';
  if (/^ssh:\/\/[^/\s]+\/\S+/i.test(u) || /^[\w.-]+@[\w.-]+:\S+/.test(u)) return 'ssh';
  return 'bad';
}
// nom du dossier proposé : dernier segment de l'URL sans « .git »
function nameFromUrl(u) {
  const m = String(u || '').trim().replace(/\/+$/, '').match(/([^/:]+?)(?:\.git)?$/);
  return m ? m[1] : '';
}
// ligne d'authentification (présence seulement, jamais de valeur) : { ok, text }
function authLine(auth, url, backend) {
  const k = gitUrlKind(url), a = auth || {};
  if (k === 'https') return a.token ? { ok: true, text: '🔑 jeton GitHub (config.conf) ✓' } : { ok: false, text: '⚠ dépôt public seulement — ajouter un jeton (⚙ config.conf)' };
  if (k === 'ssh') {
    const ks = a.sshKey || {};
    return ks[backend] || ks.host ? { ok: true, text: '🔑 clé ssh du ' + (ks[backend] ? 'backend' : 'poste') + ' ✓' } : { ok: false, text: '⚠ pas de clé ssh — ajouter une clé (⚙ ce poste)' };
  }
  return { ok: true, text: k === 'bad' ? 'URL non acceptée (https://… ou git@hôte:chemin)' : '' };
}
// avancement du job (`newJob` de l'instantané) : { state, pct (0-100), label, msg }.
// `step` est déjà un libellé français émis par le serveur (préparation, réception, résolution des deltas,
// extraction, terminé) : affiché tel quel.
function jobView(j) {
  if (!j) return null;
  const pct = Math.max(0, Math.min(100, Math.round(Number(j.pct) || 0)));
  return { state: j.state || 'running', pct, label: (j.step || 'clonage') + ' — ' + pct + ' %', msg: j.msg || '' };
}
// Job disparu de l'instantané (retiré après la durée de garde, ou reconnexion tardive) : vrai quand le panneau doit
// cesser d'attendre — dès qu'il l'a vu une fois, sinon après `graceMs` sans le voir (le premier instantané peut
// précéder l'enregistrement du job).
function jobGone(seen, firstMissAt, now, graceMs) {
  if (firstMissAt == null) return false;
  return !!seen || now - firstMissAt >= graceMs;
}
// ── Approbation de l'environnement d'un dépôt cloné (#215) ───────────────────
// Fonctions pures (testdata/env_trust.js). TOUT texte venant du dépôt passe par safeText puis textContent
// (jamais innerHTML) : aucun HTML interprété ; caractères de contrôle (séquences de terminal) rendus visibles.
function safeText(v) {
  return String(v == null ? '' : v).replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f\u061c\u200b-\u200f\u2028\u2029\u202a-\u202e\u2066-\u2069\ufeff]/g,
    (c) => (c === '\u001b' ? '\u241b' : '\u00b7'));
}
// Lignes du résumé : [{ label, items:[texte], tone:'' | 'warn' | 'err' }] — vide = rien demandé par le dépôt.
function trustRows(info) {
  const sp = (info && info.spec) || {}, rows = [];
  const add = (label, items, tone) => { if (items && items.length) rows.push({ label, items: items.map(safeText), tone: tone || '' }); };
  if (sp.image) add('Image', [sp.image + (info.chosenImage && info.chosenImage !== sp.image ? ' (au lieu de ' + info.chosenImage + ' choisie)' : '')]);
  add('Paquets', sp.packages);
  add('Paquets par gestionnaire', Object.keys(sp.packagesByManager || {}).sort().map((k) => k + ' : ' + [].concat(sp.packagesByManager[k] || []).join(', ')));
  add('setup', sp.setup, 'warn'); add('on_start', sp.onStart, 'warn'); add('watch', sp.watch);
  add('Montages', (sp.mounts || []).map((m) => (m.path || '') + (m.ro ? ' (lecture)' : ' (écriture)')), 'err');
  add('Scripts du dépôt', (info && info.risk && info.risk.scripts) || [], 'err');
  add('local', sp.local); if (sp.prefer) add('Backend préféré', [sp.prefer]);
  return rows;
}
const trustNote = (info) => {
  const r = (info && info.risk) || {}, n = [];
  if (r.exec) n.push(r.exec + ' commande' + (r.exec > 1 ? 's' : '') + ' exécutée' + (r.exec > 1 ? 's' : '') + ' dans le conteneur');
  if (r.hostMounts) n.push(r.hostMounts + ' montage' + (r.hostMounts > 1 ? 's' : '') + ' hors du projet');
  if (r.packages) n.push(r.packages + ' paquet' + (r.packages > 1 ? 's' : ''));
  if (r.watch) n.push(r.watch + ' surveillance' + (r.watch > 1 ? 's' : ''));
  if (r.local) n.push(r.local + ' entrée' + (r.local > 1 ? 's' : '') + ' locale' + (r.local > 1 ? 's' : ''));
  return n.join(' · ');
};
async function envTrustBox(src) {
  const load = async (path) => call('envTrustInfo', { path });
  const info = typeof src === 'string' ? await load(src) : src;
  if (!info || !info.path) { if (info && info.ok === false) flash('⚠ ' + safeText(info.msg || 'environnement illisible')); return; }
  const name = String(info.path).split(/[\\/]/).filter(Boolean).pop() || info.path;
  const draw = (info, note) => openModal((b) => {
    const usr = !!(whoSpace && whoSpace.admin === false); // projet d'un utilisateur : fichiers lus dans le volume du projet
    b.append(el('h2', '', (usr ? '⚠ Environnement du projet — ' : '⚠ Environnement fourni par le dépôt — ') + safeText(name)));
    b.append(el('div', 'dim', (usr ? 'Fichiers env.yaml lus dans le volume du projet.' : 'Cloné depuis ' + safeText(info.source || 'un dépôt externe') + '.') + ' Rien n\'est appliqué sans ton accord.'));
    if (info.broken) b.append(el('div', 'err', '⚠ ' + safeText(info.broken))); // registre illisible : texte du serveur, échappé
    if (info.changed === true) b.append(el('div', 'err', 'L\'environnement du dépôt a changé depuis votre approbation.'));
    if (note) b.append(el('div', 'err', note));
    const cols = el('div', 'trustcols');
    const left = el('div'); left.append(el('div', 'cap', usr ? 'Ce que le projet demande' : 'Ce que le dépôt demande'));
    const rows = trustRows(info);
    if (!rows.length) left.append(el('div', 'dim', 'rien de particulier'));
    for (const r of rows) {
      const line = el('div', 'trow'); line.append(el('span', 'tl', r.label));
      const v = el('span', 'tv ' + r.tone); for (const it of r.items) v.append(el('code', '', it));
      line.append(v); left.append(line);
    }
    const tn = trustNote(info); if (tn) left.append(el('div', 'note warn', '⚠ ' + tn + '.'));
    if (info.scriptNote) left.append(el('div', 'err', safeText(info.scriptNote))); // scripts du dépôt : non couverts par l'approbation
    const right = el('div');
    for (const [fn, txt] of Object.entries(info.files || {})) {
      right.append(el('div', 'cap', usr ? fn : '.claude-launcher/' + fn));
      right.append(txt ? el('pre', 'trustraw', safeText(txt)) : el('div', 'dim', 'absent'));
    }
    cols.append(left, right); b.appendChild(cols);
    const msg = el('div', 'err'); b.appendChild(msg);
    const btns = el('div', 'btns');
    const answer = async (choice) => {
      msg.textContent = ''; btns.querySelectorAll('button').forEach((x) => { x.disabled = true; });
      let r;
      try { r = await transport.call('envTrustAnswer', { path: info.path, choice, sha: info.sha }); } catch (e) { msg.textContent = e.message; btns.querySelectorAll('button').forEach((x) => { x.disabled = false; }); return; }
      if (r && r.ok === false) {
        if (r.retry) { const ni = await load(info.path); if (ni) draw(ni, safeText(r.msg || 'l\'environnement a changé') + ' — relire avant de décider.'); return; }
        msg.textContent = safeText(r.msg || 'refusé'); btns.querySelectorAll('button').forEach((x) => { x.disabled = false; }); return;
      }
      closeModal();
      if (r && r.tab) showView(r.tab, true);
    };
    const no = el('button', 'btn', 'Annuler'); no.onclick = () => answer('cancel');
    const ign = el('button', 'btn', 'Ouvrir sans'); ign.title = 'Socle choisi à la création, sans paquets, commandes ni montages du dépôt'; ign.onclick = () => answer('ignore');
    const acc = el('button', 'btn pri', 'Accepter et ouvrir'); acc.onclick = () => answer('accept');
    if (usr) btns.append(no, acc); else btns.append(no, ign, acc); // « Ouvrir sans » n'existe pas pour un projet d'utilisateur
    b.appendChild(btns);
    setTimeout(() => no.focus(), 0); // défaut sûr : Entrée n'accepte pas
  });
  draw(info, '');
}
// ── Effet des options de config (#226) et élément caché (#227) ─────────────
// Fonctions pures et composant commun (testdata/config_effect.js). Contrat : desktop-dispatch.md § LOT-4.
const EFFECTS = {
  immediate: { cls: 'i', label: '● immédiat' },
  project: { cls: 'p', label: '⟳ réouverture du projet' },
  claude: { cls: 'c', label: '✳ sessions claude' },
  launcher: { cls: 'l', label: '↻ relance du launcher' },
};
const effectMeta = (e) => EFFECTS[e] || null; // effet inconnu ou absent : pas de pastille
// contenu de la bulle : { meta, title, why, doText, shell, link: { kind: 'relaunch'|'refresh', text, paths } | null }
function effectBubble(x, openProjects) {
  const meta = effectMeta(x && x.effect); if (!meta) return null;
  const how = (x && x.effectHow) || {};
  const ops = openProjects || [];
  let link = null;
  if (how.action === 'relaunch') link = { kind: 'relaunch', text: '↻ Relancer le launcher', paths: [] };
  else if (how.action === 'refresh' && ops.length) link = { kind: 'refresh', text: 'Rafraîchir ' + ops.map((o) => o.name).join(', '), paths: ops.map((o) => o.path) };
  return { meta, title: x.label || x.key || '', why: how.why || (x.effect === 'immediate' ? 'appliqué dès l\'enregistrement' : ''), doText: how.do || '', shell: how.shell || '', link };
}
// rappel après enregistrement (`apply` de configApply/configSave) : { lines:[texte], actions:[{kind,text,paths}] } | null
function applyReminder(apply, labelOf) {
  const ch = ((apply && apply.changed) || []).filter((c) => c.effect && c.effect !== 'immediate');
  if (!ch.length) return null;
  const names = (e) => ch.filter((c) => c.effect === e).map((c) => (labelOf && labelOf(c.key)) || c.key);
  const ps = (apply.projects || []);
  const pn = ps.map((p) => p.name).join(', ');
  const lines = [], actions = [];
  if (names('claude').length) lines.push(names('claude').join(', ') + ' : relancer les sessions claude ouvertes' + (pn ? ' (' + pn + ')' : '') + ' pour l\'appliquer');
  if (names('project').length) lines.push(names('project').join(', ') + ' : appliqué à la prochaine ouverture du projet' + (pn ? ' (ouverts : ' + pn + ')' : ''));
  if (names('launcher').length) lines.push(names('launcher').join(', ') + ' : relancer le launcher');
  if (ps.length && (names('claude').length || names('project').length)) actions.push({ kind: 'refresh', text: 'Rafraîchir ces projets', paths: ps.map((p) => p.path) });
  if (apply.relaunch) actions.push({ kind: 'relaunch', text: '↻ Relancer le launcher', paths: [] });
  return { lines, actions };
}
function runEffectLink(l) {
  if (!l) return;
  if (l.kind === 'relaunch') { if (r1Denied('relaunch')) { flash('⚠ ' + R1_TIP); return; } relaunchBox(); }
  else if (l.kind === 'refresh') for (const p of l.paths) refreshProject(p);
}
// Œil commun à tous les éléments cachés : valeur demandée au serveur au clic seulement, gardée en mémoire le temps de
// l'affichage, re-masquée au clic, après `ms` (30 s), à la fermeture ou au changement de rubrique (eyesHide). Jamais dans un
// champ éditable ni dans le stockage du navigateur ; `onChange(state)` reçoit { shown, value, left, msg, busy }.
const EYES = new Set();
function createEye(fetchValue, onChange, opt) {
  const o = Object.assign({ ms: 30000, setT: setTimeout, clearT: clearTimeout, setI: setInterval, clearI: clearInterval }, opt || {});
  let value = null, left = 0, msg = '', busy = false, tm = null, iv = null, gen = 0;
  const state = () => ({ shown: value !== null, value, left, msg, busy });
  const emit = () => onChange(state());
  const eye = {
    state,
    hide() { gen++; o.clearT(tm); o.clearI(iv); tm = iv = null; value = null; left = 0; busy = false; emit(); },
    async toggle() {
      if (value !== null) { eye.hide(); return; }
      if (busy) return;
      busy = true; msg = ''; const my = ++gen; emit();
      let r = null;
      try { r = await fetchValue(); } catch (e) { r = { ok: false, msg: e && e.message ? e.message : 'révélation impossible' }; }
      if (my !== gen) return; // masqué ou fermé pendant l'appel : la valeur reçue est jetée
      busy = false;
      if (!r || r.ok === false || typeof r.value !== 'string') { msg = (r && r.msg) || 'révélation refusée'; value = null; emit(); return; }
      value = r.value; left = Math.round(o.ms / 1000); emit();
      tm = o.setT(() => eye.hide(), o.ms);
      iv = o.setI(() => { left = Math.max(0, left - 1); emit(); }, 1000);
    },
  };
  EYES.add(eye);
  return eye;
}
const eyesShown = () => [...EYES].some((e) => e.state().shown); // une valeur est affichée : un rafraîchissement doux de la page ne doit pas la détruire
// l'œil n'est actif que si le serveur annonce `reveal: true` (faux ou absent : accès distant ou serveur sans secretReveal)
const canReveal = (x) => !!x && x.reveal === true;
// texte d'état après enregistrement : `note` (backends.yaml, hub, env.yaml ; absent pour config.conf, remplacé par `apply`)
const savedNote = (note) => (note ? ' ' + note : '');
function eyesHide() { for (const e of [...EYES]) e.hide(); EYES.clear(); }
// Déplacer / renommer un projet (#216) ─────────────────────────────────────
// Fonctions pures (testdata/project_move.js).
// env.yaml d'un projet : lecture seule tant que le projet est ouvert (formulaire Réglages et éditeur YAML).
const ENV_LOCK_MSG = 'Fermez le projet pour modifier son environnement';
// `src` : réponse serveur (projectSettings : envLocked/envLockReason ; configOpen : readOnly/readOnlyReason), prioritaire sur la ligne.
function envLock(row, kind, src) {
  const s = src || {};
  const srv = s.envLocked === true || s.readOnly === true;
  const locked = kind === 'project-env' && (srv || !!(row && row.open));
  return { locked, why: !locked ? '' : (srv && (s.envLockReason || s.readOnlyReason)) || ENV_LOCK_MSG };
}
// refus serveur d'une écriture d'env.yaml : raison + blockers
const envRefusal = (r) => [r && r.msg].concat(((r && r.blockers) || []).filter((b) => b && b !== (r && r.msg)).map((b) => '• ' + b)).filter(Boolean).join('\n');
// Entrée de menu (arbre et ⋯ de la carte) : grisée tant que le projet est ouvert.
function moveMenuState(row) {
  const open = !!(row && row.open);
  return { disabled: open, why: open ? 'Fermez le projet pour le déplacer ou le renommer' : '' };
}
const sameLocation = (info, base, category, name) => base === info.base && (category || '') === (info.category || '') && name === info.name;
// « Ce qui change » : [{ label, old?, text, dim? }] depuis `dest` de projectMoveInfo (serveur : seules les étapes qui changent).
function moveRows(info, dest) {
  if (!dest || dest.ok === false) return [];
  const rows = [];
  for (const i of dest.impacts || []) {
    if (i.what === 'dir') rows.push({ label: 'Dossier', old: i.from, text: i.to });
    else if (i.what === 'ctr') rows.push({ label: 'Conteneur / pod', text: (i.from || '') + ' supprimé, recréé sur le nouveau chemin à la prochaine ouverture' });
    else if (i.what === 'assign') rows.push({ label: 'projects.json', text: 'affectation' + (i.value ? ' « ' + i.value + ' »' : '') + ' reportée' });
    else if (i.what === 'conf') rows.push({ label: 'config.conf', text: ((i.keys || []).join(', ') || 'entrées du projet') + ' reporté(es)' });
    else if (i.what === 'history') rows.push({ label: 'Historique claude', text: 'conversations reprises (--resume) déplacées' });
    else if (i.what === 'trust') rows.push({ label: 'Approbation', text: i.label || 'approbation de l\'environnement conservée' }); // texte du serveur, tel quel
  }
  const at = rows.findIndex((r) => r.label === 'Dossier') + 1;
  rows.splice(at, 0, { label: 'Nom', text: dest.key || info.key || '', dim: dest.keyChanged ? '(recalculé : était ' + (info.key || '') + ')' : '(inchangé)' });
  return rows;
}
// message d'échec : étape, retour arrière ou état partiel
function moveFailText(r) {
  if (!r) return 'déplacement refusé';
  let m = r.msg || 'déplacement refusé';
  if (r.step) m = 'étape « ' + r.step + ' » : ' + m;
  if (r.state === 'partial') m += ' — retour arrière incomplet' + ((r.done || []).length ? ' (étapes faites : ' + r.done.join(', ') + ')' : '') + ' : l\'état du projet est inachevé.';
  else if (r.rolledBack) m += ' — retour arrière effectué, projet inchangé.';
  return m;
}
async function moveBox(path) {
  const info = await call('projectMoveInfo', { path });
  if (!info || info.ok === false) { if (info && info.msg) flash('⚠ ' + info.msg); return; }
  const bl = await call('projectBases', {});
  const bases = ((bl && bl.bases) || []).filter((x) => x && !x.missing).map((x) => x.path);
  if (info.base && !bases.includes(info.base)) bases.unshift(info.base);
  const blockers = info.blockers || [];
  openModal((b) => {
    b.append(el('h2', '', '⇄ Déplacer / renommer — ' + info.name));
    if (blockers.length) {
      const bx = el('div', 'note warn'); bx.append(el('div', '', 'Déplacement impossible pour l\'instant :'));
      for (const x of blockers) bx.append(el('div', '', '• ' + x));
      if (info.open) {
        const fc = el('button', 'btn sm', 'Fermer le projet'); fc.onclick = () => { closeModal(); askClose(path); };
        bx.append(fc);
      }
      b.append(bx);
    }
    const pend = info.pending;
    if (pend) {
      const px = el('div', 'note warn');
      px.append(el('div', '', '⚠ Déplacement inachevé : ' + pend.from + ' → ' + pend.to));
      px.append(el('div', 'dim', 'étape en échec : ' + (pend.step || '?') + ' · faites : ' + ((pend.done || []).join(', ') || 'aucune') + (pend.msg ? ' — ' + pend.msg : '')));
      const pm = el('div', 'err');
      const act = async (method, label) => {
        px.querySelectorAll('button').forEach((x) => { x.disabled = true; }); pm.textContent = '';
        let r; try { r = await transport.call(method, { id: pend.id }); } catch (e) { r = { ok: false, msg: e.message }; }
        if (r && r.ok === true) { closeModal(); flash('✓ ' + label + ' : ' + (r.path || '')); return; }
        pm.textContent = moveFailText(r); px.querySelectorAll('button').forEach((x) => { x.disabled = false; });
      };
      const rs = el('button', 'btn sm pri', 'Reprendre'); rs.onclick = () => act('projectMoveResume', 'Déplacement repris');
      const ab = el('button', 'btn sm', 'Annuler le déplacement'); ab.onclick = () => act('projectMoveAbort', 'Déplacement annulé');
      px.append(rs, ab, pm); b.append(px);
    }
    const cols = el('div', 'trustcols');
    const left = el('div', 'form');
    const baseSel = el('select');
    for (const x of bases) { const o = el('option', '', x); o.value = x; baseSel.appendChild(o); }
    baseSel.value = info.base;
    const cat = el('select'), catNew = el('input'); catNew.placeholder = 'nouvelle catégorie (a/b : imbriquée)'; catNew.classList.add('hidden');
    const catWrap = el('div', 'catwrap'); catWrap.append(cat, catNew);
    const name = el('input'); name.value = info.name;
    const final = el('div', 'ro');
    const mkErr = () => el('div', 'fe err');
    const baseErr = mkErr(), catErr = mkErr(), nameErr = mkErr();
    left.append(el('label', '', 'Base'), baseSel, el('span'), baseErr, el('label', '', 'Catégorie'), catWrap, el('span'), catErr, el('label', '', 'Nom'), name, el('span'), nameErr, el('label', '', 'Chemin final'), final);
    const right = el('div'); right.append(el('div', 'cap', 'Ce qui change'));
    const tbl = el('div'); right.append(tbl);
    const warn = el('div', 'note warn hidden', '⚠ Changement de base : copie puis suppression (pas de simple renommage entre deux montages) — plus long.');
    right.append(warn);
    cols.append(left, right); b.appendChild(cols);
    const curCat = () => (cat.value === NEWCAT ? catNew.value.trim() : cat.value);
    const msg = el('div', 'err'); b.appendChild(msg);
    const btns = el('div', 'btns');
    const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = closeModal;
    const go = el('button', 'btn pri', 'Déplacer');
    btns.append(cancel, go); b.appendChild(btns);
    let dest = null, qseq = 0, qtm = null;
    const query = () => { // « ce qui change » : appelé à la frappe, le dernier appel gagne
      clearTimeout(qtm); const my = ++qseq;
      if (sameLocation(info, baseSel.value, curCat(), name.value.trim()) || !name.value.trim()) { dest = null; return; }
      qtm = setTimeout(async () => {
        let r = null;
        try { r = await transport.call('projectMoveInfo', { path, base: baseSel.value, category: curCat(), newCategory: cat.value === NEWCAT, name: name.value.trim() }); } catch (e) { r = null; }
        if (my !== qseq) return;
        dest = (r && r.dest) || null; render();
      }, 200);
    };
    const upd = () => { query(); render(); };
    const render = () => {
      final.textContent = joinNewPath(baseSel.value, curCat(), name.value.trim() || '<nom>');
      tbl.textContent = '';
      for (const r of moveRows(info, dest)) {
        const line = el('div', 'trow'); line.append(el('span', 'tl', r.label));
        const v = el('span', 'tv');
        if (r.old) v.append(el('code', 'old', r.old), el('span', 'dim', '→'));
        v.append(el('code', '', r.text)); if (r.dim) v.append(el('span', 'dim', r.dim));
        line.append(v); tbl.append(line);
      }
      warn.classList.toggle('hidden', !(dest && dest.crossDevice));
      const t = dest && dest.ok === false ? ({ base: baseErr, category: catErr, name: nameErr }[dest.field] || msg) : null;
      for (const e of [baseErr, catErr, nameErr]) e.textContent = '';
      if (t) t.textContent = dest.exists && !dest.msg ? 'la destination existe déjà' : (dest.msg || 'destination refusée');
      go.disabled = blockers.length > 0 || sameLocation(info, baseSel.value, curCat(), name.value.trim()) || (dest && dest.ok === false);
      go.title = blockers.length ? 'voir les blocages ci-dessus' : go.disabled ? 'même emplacement ou destination refusée' : '';
    };
    let want = info.category;
    const loadCats = makeCatLoader((bs) => transport.call('projectCategories', { base: bs }), (bs, cats) => {
      if (bs !== baseSel.value) return;
      cat.textContent = '';
      const o0 = el('option', '', '(directement sous la base)'); o0.value = ''; cat.appendChild(o0);
      const known = cats.map((c) => c.path);
      for (const c of cats) { const o = el('option', '', c.path + ' (' + c.projects + ')'); o.value = c.path; cat.appendChild(o); }
      const o1 = el('option', '', '＋ nouvelle catégorie…'); o1.value = NEWCAT; cat.appendChild(o1);
      cat.value = want && known.includes(want) ? want : ''; want = '';
      catNew.classList.toggle('hidden', cat.value !== NEWCAT); upd();
    });
    loadCats(baseSel.value);
    baseSel.onchange = () => { want = ''; baseErr.textContent = catErr.textContent = ''; cat.textContent = ''; loadCats(baseSel.value); upd(); };
    cat.onchange = () => { catErr.textContent = ''; catNew.classList.toggle('hidden', cat.value !== NEWCAT); if (cat.value === NEWCAT) catNew.focus(); upd(); };
    catNew.oninput = upd; name.oninput = () => { nameErr.textContent = ''; upd(); };
    upd();
    go.onclick = async () => {
      msg.textContent = ''; baseErr.textContent = catErr.textContent = nameErr.textContent = '';
      const ce = categoryError(cat.value === NEWCAT, catNew.value);
      if (ce) { catErr.textContent = ce; catNew.focus(); return; }
      btns.querySelectorAll('button').forEach((x) => { x.disabled = true; });
      msg.className = 'dim'; msg.textContent = 'Déplacement en cours…' + (baseSel.value !== info.base ? ' (copie : peut durer)' : '');
      let r;
      try { r = await transport.call('projectMove', { path, base: baseSel.value, category: curCat(), newCategory: cat.value === NEWCAT, name: name.value.trim() }); }
      catch (e) { r = { ok: false, msg: e.message }; }
      msg.className = 'err';
      if (r && r.ok === true) { closeModal(); flash('✓ Projet déplacé : ' + (r.path || '')); return; }
      msg.textContent = '';
      const t = { base: baseErr, category: catErr, name: nameErr }[r && r.field];
      if (t) t.textContent = (r && r.msg) || 'refusé'; else msg.textContent = moveFailText(r);
      cancel.disabled = false; go.disabled = r && r.state === 'partial'; // état partiel : ne pas relancer à l'aveugle
      if (go.disabled) go.title = 'déplacement inachevé : reprise non disponible ici';
    };
  });
}
// Proposition /init-project (évènement `initProposal`) : un bandeau par projet.
function initProposalBar(path) {
  if (!path) return;
  const id = 'initprop-' + encodeURIComponent(path);
  if ($(id)) return;
  const name = String(path).split(/[\\/]/).filter(Boolean).pop() || path;
  const b = el('div', 'lanbanner srv lbar g'); b.id = id;
  b.textContent = '◆ ' + name + ' créé depuis le template — lancer /init-project ?';
  b.append(el('span', 'sp'));
  // refus du serveur (leader pas prêt…) : le bandeau reste, le message est affiché ; la proposition est gardée côté projet
  const answer = (run) => {
    b.classList.add('busy');
    call('initProposalAnswer', { path, run }).then((r) => {
      if (r && r.ok === false) { b.classList.remove('busy'); flash('⚠ ' + (r.msg || 'proposition refusée')); } else b.remove();
    }, () => b.classList.remove('busy'));
  };
  const yes = el('button', 'btn sm pri', 'Lancer /init-project'); yes.onclick = () => answer(true);
  const no = el('button', 'btn sm', 'Plus tard'); no.onclick = () => answer(false);
  b.append(yes, no); $('main').insertBefore(b, $('tabs'));
}
// Job de création en cours : l'instantané le pousse au dialogue ouvert (S.newDlg).
function newJobSync(j) { S.newJob = j || null; if (S.newDlg) S.newDlg(S.newJob); }

// Les 3 sources (#215) : Projet brut (dossier + git init, remote si fourni, jamais de /init-project), Template projet
// (idem + /init-project proposé à la 1re ouverture), Dépôt Git. Plus d'option « géré » ni de CLAUDE.md minimal (raw).
const NEW_SOURCES = [
  ['empty', '▢ Projet brut', 'Dossier et git init ; lien vers le remote seulement si une URL est fournie. Pas de /init-project.'],
  ['template', '◆ Template projet', 'Comme brut, puis /init-project proposé à la 1re ouverture.'],
  ['git', '⎇ Dépôt Git', 'Clonage d\'une URL. HTTPS : jeton de config.conf · ssh : clé du backend.'],
];
// arguments de `create` propres à la source
function sourceArgs(src, url, remote) {
  if (src === 'git') return { url: String(url || '').trim() };
  return { raw: false, remote: String(remote || '').trim() };
}

// création d'un projet : base, catégorie, nom, source (brut, template, dépôt Git), exécution
// `pre` : dossier de l'arbre (base ou catégorie) à pré-sélectionner, '' sinon.
// Utilisateur (mode serveur) : un projet naît sur UN de SES backends (backend, catégorie, nom), jamais sur un dossier du serveur (#275).
function newUserProject() {
  const bs = (whoSpace && whoSpace.backends) || [];
  if (!bs.length) { flash('⚠ Ajoute d\'abord un backend (Réglages › Backends)'); if (typeof openSettings === 'function') openSettings('', 'backends'); return; }
  openModal((b) => {
    b.append(el('h2', '', 'Nouveau projet'));
    const f = el('div', 'form');
    const errs = {};
    const field = (key, label, ctl) => { f.append(el('label', '', label), ctl); const w = el('div', 'fe err'); f.append(el('span', ''), w); errs[key] = w; };
    const be = el('select'); for (const x of bs) { const o = el('option', '', x.name + (x.type ? ' (' + x.type + ')' : '')); o.value = x.name; be.appendChild(o); }
    field('backend', 'Backend', be);
    const cat = el('input'); cat.placeholder = 'catégorie (a-b ; a/b : imbriquée)'; cat.spellcheck = false;
    field('category', 'Catégorie', cat);
    const name = el('input'); name.placeholder = 'nom du projet'; name.spellcheck = false;
    field('name', 'Nom', name);
    const final = el('div', 'ro'); f.append(el('label', '', 'Emplacement'), final);
    const upd = () => { final.textContent = (be.value || '?') + ' / ' + (cat.value.trim() || '<catégorie>') + ' / ' + (name.value.trim() || '<nom>'); };
    for (const n of [be, cat, name]) n.addEventListener('input', upd); upd();
    b.appendChild(f);
    const err = el('div', 'err'); b.appendChild(err);
    const why = el('div', 'dim'); b.appendChild(why);
    const btns = el('div', 'btns');
    const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = closeModal;
    const ok = el('button', 'btn pri', 'Créer');
    // jamais de bouton grisé muet : la raison est écrite à côté
    const gate = () => {
      const r = !cat.value.trim() ? 'saisis une catégorie' : !name.value.trim() ? 'saisis un nom' : '';
      ok.disabled = !!r; why.textContent = r;
    };
    for (const n of [cat, name]) n.addEventListener('input', () => { for (const k of Object.keys(errs)) errs[k].textContent = ''; err.textContent = ''; gate(); });
    gate();
    const showFail = (r) => { const m = (r && r.msg) || 'création refusée'; const t = errs[r && r.field]; if (t) t.textContent = m; else err.textContent = m; };
    ok.onclick = async () => {
      for (const k of Object.keys(errs)) errs[k].textContent = ''; err.textContent = '';
      const args = { backend: be.value, category: cat.value.trim(), name: name.value.trim() }; // création sur le backend ; le clonage dans le conteneur viendra ensuite
      ok.disabled = true;
      let r; try { r = await transport.call('create', args); } catch (e) { r = { ok: false, msg: e.message }; }
      if (r && r.ok === false) { showFail(r); gate(); return; }
      closeModal();
      if (r && r.job) { flash('Clonage en cours…'); return; }
      if (r && r.path) { S.selectAfter = r.path; if (rowOf(r.path)) { S.selectAfter = ''; select(r.path); } } // le projet créé est sélectionné dès que sa ligne arrive dans l'arbre
      if (r && r.tab) showView(r.tab, true);
    };
    btns.append(cancel, ok); b.appendChild(btns);
    cat.focus();
  });
}

async function newProject(pre) {
  if (whoSpace && whoSpace.admin === false) return newUserProject();
  const info = await call('newInfo', {});
  const bases = (info.bases || (info.roots || []).map((r) => ({ path: r }))).filter((x) => x && !x.missing).map((x) => x.path);
  const sp = splitPreselect(bases, pre);
  openModal((b) => {
    b.append(el('h2', '', 'Nouveau projet'));
    const f = el('div', 'form');
    // 1 · source : cartes Vide / Git / Template
    b.append(el('div', 'cap', '1 · Créer à partir de'));
    const cards = el('div', 'srccards');
    let src = 'empty';
    const cardEls = {};
    for (const [v, t, d] of NEW_SOURCES) {
      const c = el('div', 'card'); c.append(el('h4', '', t), el('p', '', d)); c.onclick = () => setSrc(v); cards.appendChild(c); cardEls[v] = c;
    }
    b.appendChild(cards);
    const urlBox = el('div', 'urlbox');
    const url = el('input'); url.placeholder = 'https://github.com/moi/projet.git ou git@github.com:moi/projet.git';
    const urlErr = el('div', 'fe err'); const auth = el('div', 'authline');
    urlBox.append(el('label', 'l', 'URL du dépôt'), url, urlErr, auth);
    const tplBox = el('div', 'urlbox dim'); tplBox.textContent = 'Template adopté : ' + (info.template || 'version inconnue') + ' — dossier et git init (remote si fourni), /init-project proposé à la première ouverture.';
    b.append(urlBox, tplBox);
    b.append(el('div', 'cap', '2 · Emplacement'));
    const field = (label, ctl) => { f.append(el('label', '', label), ctl); const w = el('div', 'fe err'); f.append(el('span', ''), w); return w; };
    const base = el('select');
    for (const x of bases) { const o = el('option', '', x); o.value = x; base.appendChild(o); }
    base.value = sp.base || info.default || bases[0] || '';
    const baseErr = field('Base', base);
    const cat = el('select');
    const catNew = el('input'); catNew.placeholder = 'nouvelle catégorie (a/b : imbriquée)'; catNew.classList.add('hidden');
    const catWrap = el('div', 'catwrap'); catWrap.append(cat, catNew);
    const catErr = field('Catégorie', catWrap);
    const name = el('input'); name.placeholder = 'nom du dossier';
    const nameErr = field('Nom', name);
    const final = el('div', 'ro');
    f.append(el('label', '', 'Chemin'), final);
    const curCat = () => (cat.value === NEWCAT ? catNew.value.trim() : cat.value);
    const upd = () => { final.textContent = base.value ? joinNewPath(base.value, curCat(), name.value.trim() || '<nom>') : '—'; };
    let want = sp.category; // catégorie à sélectionner au prochain rechargement (pré-sélection)
    const loadCats = makeCatLoader((bs) => transport.call('projectCategories', { base: bs }), (bs, cats) => {
      if (bs !== base.value) return;
      cat.textContent = '';
      const o0 = el('option', '', '(directement sous la base)'); o0.value = ''; cat.appendChild(o0);
      const known = cats.map((c) => c.path);
      for (const c of cats) { const o = el('option', '', c.path + ' (' + c.projects + ')'); o.value = c.path; cat.appendChild(o); }
      const o1 = el('option', '', '＋ nouvelle catégorie…'); o1.value = NEWCAT; cat.appendChild(o1);
      cat.value = want && known.includes(want) ? want : ''; want = '';
      catNew.classList.toggle('hidden', cat.value !== NEWCAT); upd();
    });
    if (base.value) loadCats(base.value);
    base.onchange = () => { want = ''; baseErr.textContent = ''; catErr.textContent = ''; cat.textContent = ''; loadCats(base.value); upd(); };
    cat.onchange = () => { catErr.textContent = ''; catNew.classList.toggle('hidden', cat.value !== NEWCAT); if (cat.value === NEWCAT) catNew.focus(); upd(); };
    catNew.oninput = upd; name.oninput = () => { nameErr.textContent = ''; upd(); };
    upd();
    const remote = el('input'); remote.placeholder = 'URL du remote, ex. git@github.com:moi/projet.git (facultatif : sans URL, pas de remote)';
    const remoteL = el('label', '', 'Remote git');
    f.append(remoteL, remote);
    const ex = execFields(info, f);
    let nameTouched = false;
    const updAuth = () => { const a = authLine(info.auth, url.value, ex().backend || ''); auth.textContent = a.text; auth.className = 'authline ' + (a.ok ? 'ok' : 'warn'); };
    const setSrc = (v) => {
      src = v;
      for (const k of Object.keys(cardEls)) cardEls[k].classList.toggle('on', k === v);
      urlBox.classList.toggle('hidden', v !== 'git'); tplBox.classList.toggle('hidden', v !== 'template');
      for (const x of [remoteL, remote]) x.classList.toggle('hidden', v === 'git');
      if (v === 'git') { updAuth(); setTimeout(() => url.focus(), 0); }
    };
    url.oninput = () => {
      urlErr.textContent = ''; updAuth();
      if (!nameTouched) { name.value = nameFromUrl(url.value); nameErr.textContent = ''; upd(); }
    };
    f.addEventListener('change', updAuth);
    name.addEventListener('input', () => { nameTouched = true; });
    setSrc('empty');
    b.appendChild(f);
    const prog = el('div', 'newprog hidden'); b.appendChild(prog);
    const err = el('div', 'err'); b.appendChild(err);
    const btns = el('div', 'btns');
    const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = closeModal;
    const ok = el('button', 'btn pri', 'Créer et ouvrir');
    const showFail = (r) => {
      const m = (r && r.msg) || 'création refusée';
      const t = { base: baseErr, category: catErr, name: nameErr }[r && r.field];
      if (t) t.textContent = m; else err.textContent = m;
    };
    ok.onclick = async () => {
      err.textContent = ''; baseErr.textContent = catErr.textContent = nameErr.textContent = '';
      const ce = categoryError(cat.value === NEWCAT, catNew.value);
      if (ce) { catErr.textContent = ce; catNew.focus(); return; }
      if (src === 'git') {
        const k = gitUrlKind(url.value);
        if (k === '' || k === 'bad') { urlErr.textContent = k ? 'URL non acceptée (https://… ou git@hôte:chemin)' : 'URL du dépôt requise'; url.focus(); return; }
      }
      const args = { base: base.value, category: curCat(), newCategory: cat.value === NEWCAT, name: name.value, source: src };
      Object.assign(args, sourceArgs(src, url.value, remote.value));
      try {
        const r = await transport.call('create', Object.assign(args, ex()));
        if (r && r.ok === false) { showFail(r); return; }
        if (r && r.job) { watchJob(r.job); return; }
        closeModal();
        if (r && r.tab) showView(r.tab, true);
      } catch (e) { showFail({ msg: e.message }); }
    };
    // clonage : le dialogue montre l'avancement (instantané `newJob`), Annuler tue le clone ; échec : retour au formulaire
    const watchJob = (id) => {
      f.classList.add('hidden'); b.querySelectorAll('.cap, .srccards, .urlbox').forEach((x) => x.classList.add('hidden'));
      btns.classList.add('hidden'); prog.classList.remove('hidden');
      let seen = false, missAt = null, tm = null;
      const render = (j) => {
        clearTimeout(tm);
        if (!j || j.id !== id) {
          if (missAt == null) missAt = Date.now();
          if (!jobGone(seen, missAt, Date.now(), 3000)) { tm = setTimeout(() => S.newDlg && S.newDlg(S.newJob), 3100); return; }
          // job retiré sans état final vu : le panneau ne reste pas figé
          S.newDlg = null; prog.textContent = '';
          const close = el('button', 'btn pri', 'Fermer'); close.onclick = closeModal;
          prog.append(el('h3', '', '⎇ Création : état inconnu'), el('div', 'dim', 'Le serveur ne signale plus ce clonage (connexion coupée ou relance). En cas d\'échec le dossier est supprimé : vérifier l\'arbre des projets.'), el('div', 'btns', ''));
          prog.lastChild.appendChild(close); return;
        }
        seen = true; missAt = null;
        const v = jobView(j);
        prog.textContent = '';
        if (v.state === 'done') { S.newDlg = null; closeModal(); return; }
        if (v.state === 'failed') {
          S.newDlg = null;
          const e = el('div', 'err', v.msg || 'création échouée'); const back = el('button', 'btn pri', 'Revenir au formulaire');
          back.onclick = () => { prog.classList.add('hidden'); f.classList.remove('hidden'); btns.classList.remove('hidden'); b.querySelectorAll('.cap, .srccards').forEach((x) => x.classList.remove('hidden')); setSrc(src); };
          const close = el('button', 'btn', 'Fermer'); close.onclick = closeModal;
          prog.append(el('h3', '', '⎇ Création de ' + (j.name || '') + ' échouée'), e, el('div', 'btns', ''));
          prog.lastChild.append(close, back); return;
        }
        const bar = el('div', 'pbar'); const fill = el('span', ''); fill.style.width = v.pct + '%'; bar.appendChild(fill);
        const cn = el('button', 'btn', 'Annuler le clonage'); cn.onclick = () => { cn.disabled = true; call('createCancel', { job: id }).catch(() => {}); };
        prog.append(el('h3', '', '⎇ Clonage de ' + (j.name || name.value) + '…'), el('div', 'dim', v.label), bar, el('div', 'btns', ''));
        prog.lastChild.appendChild(cn);
      };
      S.newDlg = render; render(S.newJob);
    };
    btns.append(cancel, ok); b.appendChild(btns);
    b.onkeydown = (e) => { if (e.key === 'Enter' && e.target.tagName !== 'SELECT') { e.preventDefault(); ok.click(); } };
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
  if (r && r.open) { flash('F2 : ' + ENV_LOCK_MSG); return; }
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
  CE.lock = envLock((S.snap.rows || []).find((x) => x.path === CE.project), d.kind, d); // #216 : projet ouvert = lecture seule
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
  ta.wrap = 'off'; ta.readOnly = !!(CE.lock && CE.lock.locked);
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
  if (CE.lock && CE.lock.locked) { $('cesave').disabled = true; $('cesave').title = CE.lock.why; $('cestatus').textContent = '🔒 ' + CE.lock.why + ' (lecture seule).'; }
  if (d.fromTemp && !(CE.lock && CE.lock.locked)) $('cestatus').textContent = 'Fichier absent : modèle proposé, créé à l\'enregistrement.';
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
  const lk = !!(CE.lock && CE.lock.locked);
  const r1 = r1Denied('configSave', CE.doc && CE.doc.kind);
  $('cesave').disabled = errs > 0 || lk || r1;
  $('cesave').title = lk ? CE.lock.why : errs ? 'Corriger les erreurs avant d\'enregistrer (le brouillon reste ici)' : '';
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
  if (r && r.ok === false) { $('cestatus').textContent = ''; $('cestatus').append(el('span', 't-red pre', '✗ ' + (envRefusal(r) || 'enregistrement refusé') + '\nRien n\'est enregistré.')); return; } // refus serveur : autre launcher, conteneur actif…
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
  $('cestatus').textContent = '✓ Enregistré (version précédente gardée en .bak).' + savedNote(r.note);
  const rem = applyReminder(r.apply, null); // #226 : rappel des actions pour les options modifiées
  if (rem) { $('cestatus').append(el('span', 't-yellow', ' ' + rem.lines.join(' · ') + ' ')); for (const a of rem.actions) { const bt = el('button', 'btn sm', a.text); bt.onclick = () => runEffectLink(a); $('cestatus').append(bt); } }
  if (r.warn) $('cestatus').append(el('span', 't-orange', ' ⚠ ' + r.warn)); // enregistré avec avertissement
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
$('who').addEventListener('click', whoMenu); $('who').addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') whoMenu(e); });
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
  attnSync();
  if (!S.cursor || (S.cursor !== 'new' && !s.rows.find((r) => r.path === S.cursor))) S.cursor = s.selected || (projectRows()[0] || {}).path || 'new';
  // onglets disparus (session terminée, détachée)
  const open = new Set((s.tabs || []).map((t) => t.path));
  for (const p of Object.keys(S.views)) if (!open.has(p) && !(S.grace && S.grace.pending.has(p))) { dropView(p); if (S.view === p) S.view = 'leaders'; }
  for (const p of open) if (!S.views[p]) ensureView(p);
  if (S.view !== 'leaders' && !open.has(S.view) && !(S.grace && S.grace.pending.has(S.view))) S.view = 'leaders';
  renderHead(); renderTree(); renderTabs(); lanBanner(); launcherBar(); newJobSync(s.newJob);
  if (S.selectAfter && (s.rows || []).some((x) => x.path === S.selectAfter)) { const p0 = S.selectAfter; S.selectAfter = ''; select(p0); }
  if (typeof dwRefresh === 'function') dwRefresh();
  for (const [p, v] of Object.entries(S.views)) v.root.classList.toggle('hidden', p !== S.view);
  leaders.root.classList.toggle('hidden', S.view !== 'leaders');
  if (S.view === 'leaders') leaders.layout();
  leaders.attnAll();
  graceStep();
  refusedSync(s.tabs);
}

// ── Attente du teamleader : notifications navigateur (#223, contracts/attention.md § 7) ──
// Fenêtre native : notification système par le Go, rien ici. Option ATTENTION_NOTIFY lue dans l'instantané
// (`attentionNotify`, absent = défaut activé). Une notification au plus par `id`, fermée à la fin de l'attente.
const attnNotes = {}; // id -> Notification
const ATTN_BASE_TITLE = document.title;
function attnNotifyOn() { return !(S.snap && S.snap.attentionNotify === false); }
function attnCanNotify() { return isWebClient() && 'Notification' in window && window.isSecureContext !== false; }
function attnClose(id) { const n = attnNotes[id]; if (n) { try { n.close(); } catch (e) { /* déjà fermée */ } delete attnNotes[id]; } }
function attnShow(d) {
  if (!d || !d.id || attnNotes[d.id] || !attnNotifyOn() || Notification.permission !== 'granted') return;
  try {
    const n = new Notification(d.name + ' : le teamleader attend une réponse', { body: (d.kind === 'permission' ? 'Permission : ' : '') + (d.text || ''), tag: d.id });
    n.onclick = () => { try { window.focus(); } catch (e) { /* refusé */ } if (d.path && rowOf(d.path) && S.snap.tabs.some((t) => t.path === d.path)) showView(d.path, true); n.close(); };
    n.onclose = () => { if (attnNotes[d.id] === n) delete attnNotes[d.id]; };
    attnNotes[d.id] = n;
  } catch (e) { /* notification refusée par le navigateur : badge seul */ }
}
function attnPending() { return (S.attnLive || []).filter((d) => !attnNotes[d.id]); }
function attnSync() {
  // titre de page « ❓ » tant qu'au moins un teamleader attend (badge toujours affiché, indépendant de l'option)
  document.title = (attnAny(S.snap) ? '❓ ' : '') + ATTN_BASE_TITLE;
  // l'instantané fait foi : une attente connue des évènements mais absente de l'instantané est terminée
  const ids = new Set(); for (const t of (S.snap.tabs || [])) if (t.attention) ids.add(t.attention.id);
  for (const id of Object.keys(attnNotes)) if (!ids.has(id)) attnClose(id);
  attnBar();
}
// Bandeau d'autorisation : option activée, permission jamais décidée, contexte sûr. Le clic est le geste exigé.
function attnBar() {
  let b = $('attnbar');
  const want = attnCanNotify() && attnNotifyOn() && Notification.permission === 'default' && !store.get('attnBarOff', false);
  if (!want) { if (b) b.remove(); return; }
  if (b) return;
  b = el('div', 'lanbanner srv attnbar'); b.id = 'attnbar';
  b.append(el('span', '', '❓ Être prévenu quand un teamleader attend une réponse ?'), el('span', 'sp'));
  const ok = el('button', 'btn sm pri', 'Autoriser les notifications d\'attente');
  ok.onclick = () => {
    const done = (p) => { b.remove(); if (p === 'granted') for (const d of S.attnLive || []) attnShow(d); };
    try { const r = Notification.requestPermission(done); if (r && r.then) r.then(done); } catch (e) { b.remove(); }
  };
  const x = el('button', 'btn sm', '✕'); x.title = 'Ne plus proposer'; x.onclick = () => { store.set('attnBarOff', true); b.remove(); };
  b.append(ok, x);
  $('main').insertBefore(b, $('tabs'));
}
function onAttention(d) {
  if (!d) return;
  S.attnLive = (S.attnLive || []).filter((x) => x.path !== d.path).concat([d]);
  if (attnCanNotify() && attnNotifyOn()) { attnShow(d); attnBar(); }
}
function onAttentionEnd(d) {
  if (!d) return;
  S.attnLive = (S.attnLive || []).filter((x) => x.id !== d.id);
  attnClose(d.id);
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

// Binaire du launcher changé (#219) : `launcher{binChanged}` de l'instantané.
// Fonction pure : null (rien à dire), ou { kind: 'g', text } — bandeau vert : binaire remplacé.
function launcherBarState(l) {
  if (!l || !l.binChanged) return null;
  return { kind: 'g', text: '✓ nouvelle version installée — l\'interface tourne encore avec l\'ancienne' };
}
function launcherBar() {
  const st = launcherBarState(S.snap && S.snap.launcher);
  let b = $('launchbar');
  const key = st ? st.kind + st.text : '';
  if (!st || S.launchDismiss === key) { if (b) b.remove(); return; }
  if (b && b.dataset.key === key) return;
  if (!b) { b = el('div', 'lanbanner'); b.id = 'launchbar'; $('main').insertBefore(b, $('tabs')); }
  b.className = 'lanbanner srv lbar ' + st.kind; b.dataset.key = key; b.textContent = st.text;
  b.append(el('span', 'sp'));
  const r = el('button', 'btn sm pri', '↻ Relancer'); r.onclick = relaunchBox; b.appendChild(r);
  const x = el('button', 'btn sm', '✕'); x.title = 'Masquer'; x.onclick = () => { S.launchDismiss = key; b.remove(); }; b.appendChild(x);
}
async function relaunchBox() {
  let i = {};
  try { i = (await transport.call('launcherInfo')) || {}; } catch (e) { /* dialogue sans détail */ }
  openModal((b) => {
    b.append(el('h2', '', '↻ Relancer le launcher'));
    b.append(el('h3', '', 'Relancé'));
    const ul = el('div', '');
    const ver = i.running && i.installed ? ' (' + verTxt(i.running) + ' → ' + verTxt(i.installed) + ')' : '';
    ul.append(el('div', '', '• interface' + (i.mode === 'pod' ? ' (redémarrage du pod du launcher, elle revient seule)' : '') + ver));
    b.append(ul, el('h3', '', 'Gardé'), el('div', 't-green', '• sessions des projets (tmux), conteneurs et pods'));
    const msg = el('div', 't-red'); b.append(msg);
    const btns = el('div', 'btns');
    const no = el('button', 'btn', 'Annuler (N)'); no.onclick = closeModal;
    const yes = el('button', 'btn pri', 'Relancer (O)');
    yes.onclick = async () => {
      yes.disabled = true; msg.textContent = '';
      try {
        const r = await transport.call('relaunch', { confirm: true });
        if (r && r.ok === false) { msg.textContent = r.msg || 'relance refusée'; yes.disabled = false; return; }
        closeModal(); // l'interface se reconnecte seule (flux, bandeau « connexion perdue »)
      } catch (e) { closeModal(); } // coupure attendue : le serveur quitte
    };
    btns.append(no, yes); b.appendChild(btns);
    b.onkeydown = (e) => { const k = e.key.toLowerCase(); if (k === 'o' || k === 'y') { e.preventDefault(); yes.click(); } else if (k === 'n') { e.preventDefault(); closeModal(); } };
  });
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
  r1Info(r);
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
    case 'attention': onAttention(data); break; // #223
    case 'attentionEnd': onAttentionEnd(data); break;
    case 'attentionFocus': if (data && data.path && (S.snap.tabs || []).some((t) => t.path === data.path)) showView(data.path, true); break; // clic sur la notification Windows
    case 'envTrust': if (data && data.path) envTrustBox(data); break; // #215 : approbation de l'environnement du dépôt
    case 'initProposal': initProposalBar(data && data.path); break; // #215 : proposition /init-project (projet Template)
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
// #242 : la zone des vues change de hauteur sans évènement de fenêtre (bandeau d'attente ou de notifications qui
// apparaît ou disparaît) : fit puis nouvelle réduction éventuelle des terminaux, sans attendre le layout suivant
if (typeof ResizeObserver === 'function') {
  let vw = -1, vh = -1;
  new ResizeObserver((es) => {
    const w = Math.round(es[0].contentRect.width), h = Math.round(es[0].contentRect.height);
    if (w === vw && h === vh) return;
    vw = w; vh = h;
    scheduleFit();
    requestAnimationFrame(() => { for (const v of Object.values(S.views)) if (v.layout) v.applyLayout(v.layout); });
  }).observe($('views'));
}
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
