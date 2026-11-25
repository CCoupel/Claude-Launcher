// E2E #172 / #251 / #252 — voir run.sh. Proxy factice (Host et Origin réécrits comme le ferait l'Ingress + oauth2-proxy),
// Chrome sans tête piloté par CDP (WebSocket natif de node ≥ 22). Un scénario = une identité posée par le proxy.
import http from 'node:http';
import { spawn } from 'node:child_process';

const { LPORT, PPORT, CPORT, EXT, INT, CHROME, WORK } = process.env;
const state = { user: 'ccoupel', host: EXT, expired: false };
const results = [];
const check = (name, ok, detail = '') => { results.push([name, ok]); console.log(`${ok ? 'VERT ' : 'ROUGE'} ${name}${ok ? '' : ' — ' + detail}`); };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ── proxy factice ───────────────────────────────────────────────────────────────────────────────────────────────
const proxy = http.createServer((req, res) => {
  const path = req.url || '/';
  if (path.startsWith('/oauth2/sign_out')) { // oauth2-proxy : efface son cookie puis redirige vers rd
    res.writeHead(302, { Location: '/signed-out' }); res.end(); return;
  }
  const open = path === '/signed-out' && req.method === 'GET'; // route exemptée d'authentification
  if (state.expired && path.startsWith('/api/')) { res.writeHead(401); res.end('session expirée'); return; }
  const headers = { ...req.headers, host: state.host };
  delete headers['x-forwarded-user'];
  if (state.user && !open) headers['x-forwarded-user'] = state.user;
  if (headers.origin) headers.origin = 'https://' + state.host;
  headers['x-forwarded-for'] = '192.168.1.20';
  const up = http.request({ host: '127.0.0.1', port: LPORT, path, method: req.method, headers }, (r) => {
    res.writeHead(r.statusCode, r.headers); r.pipe(res);
  });
  up.on('error', () => { res.writeHead(502); res.end(); });
  res.on('close', () => up.destroy());
  req.pipe(up);
});
await new Promise((r) => proxy.listen(PPORT, '127.0.0.1', r));
const base = `http://127.0.0.1:${PPORT}`;

// ── navigateur ──────────────────────────────────────────────────────────────────────────────────────────────────
const chrome = spawn(CHROME, ['--no-sandbox', '--disable-gpu', `--user-data-dir=${WORK}/chrome`, `--remote-debugging-port=${CPORT}`,
  '--window-size=1280,900', 'about:blank'], { stdio: 'ignore' });
let ws, nextId = 1; const pending = new Map();
for (let i = 0; i < 100 && !ws; i++) {
  try {
    const tabs = await (await fetch(`http://127.0.0.1:${CPORT}/json`)).json();
    const t = tabs.find((x) => x.type === 'page');
    if (t) { ws = new WebSocket(t.webSocketDebuggerUrl); await new Promise((ok, ko) => { ws.onopen = ok; ws.onerror = ko; }); }
  } catch { ws = undefined; await sleep(200); }
}
if (!ws) { console.log('ERREUR : navigateur injoignable'); process.exit(2); }
ws.onmessage = (m) => { const d = JSON.parse(m.data); if (d.id && pending.has(d.id)) { pending.get(d.id)(d); pending.delete(d.id); } };
const cdp = (method, params = {}) => new Promise((r) => { const id = nextId++; pending.set(id, r); ws.send(JSON.stringify({ id, method, params })); });
const evalJs = async (expr) => { const r = await cdp('Runtime.evaluate', { expression: expr, returnByValue: true, awaitPromise: true }); return r.result?.result?.value; };
const until = async (expr, ms = 15000) => { const end = Date.now() + ms; while (Date.now() < end) { if (await evalJs(expr)) return true; await sleep(150); } return false; };
const open = async (url) => { await cdp('Page.navigate', { url }); await sleep(400); };
await cdp('Page.enable'); await cdp('Runtime.enable');

try {
  // 0. sans identité : le serveur refuse (401), la page n'est pas l'interface
  state.user = '';
  let r = await fetch(base + '/', { redirect: 'manual' });
  check('sans identité : 401', r.status === 401 && r.headers.get('x-launcher-refusal') === 'identity', `HTTP ${r.status}`);
  r = await fetch(base + '/signed-out');
  check('/signed-out sans identité : page servie', r.status === 200 && (await r.text()).includes('Se reconnecter'));

  // 1. administrateur : interface, pastille, menu, déconnexion
  state.user = 'ccoupel'; state.host = EXT; state.expired = false;
  await open(base + '/');
  check('smoke : interface chargée derrière le proxy', await until(`!!document.getElementById('version') && document.getElementById('version').textContent.length > 0`));
  check('identité affichée (pastille)', await until(`(document.getElementById('who')||{}).textContent?.includes('ccoupel') && !document.getElementById('who').classList.contains('hidden')`));
  await evalJs(`document.getElementById('who').click()`);
  const menu = await until(`!!document.querySelector('#ctxmenu.whomenu')`) ? await evalJs(`document.querySelector('#ctxmenu.whomenu').textContent`) : '';
  check('menu identité : GitHub, rôle, déconnexion', menu.includes('Connecté avec GitHub') && menu.includes('administrateur') && menu.includes('Se déconnecter'), menu);
  await evalJs(`[...document.querySelectorAll('#ctxmenu div')].find(d => d.textContent.includes('Se déconnecter')).click()`);
  check('déconnexion : /oauth2/sign_out puis /signed-out', await until(`location.pathname === '/signed-out'`, 8000) && (await evalJs(`document.body.textContent`)).includes('Se reconnecter'));

  // 2. rôle user : Réglages en lecture seule, actions grisées
  state.user = 'alice'; await open(base + '/');
  await until(`(document.getElementById('who')||{}).textContent?.includes('alice')`);
  await evalJs(`document.getElementById('cfgbtn').click()`);
  const ro = await until(`!!document.querySelector('#modalbox .cehead h2') && document.querySelector('#modalbox .cehead h2').textContent.includes('lecture seule')`, 10000);
  check('rôle user : « lecture seule » dans le titre des Réglages', ro);
  const off = await evalJs(`(() => { const b=[...document.querySelectorAll('#modalbox button, #modalbox input, #modalbox select, #modalbox textarea')].filter(x=>!x.closest('.cehead, .strail')); return b.length > 0 && b.every(x => x.disabled) && b.some(x => x.title.includes('réservé à un administrateur')); })()`);
  check('rôle user : actions R1 grisées avec infobulle', off === true);

  // 3. compte local admin@local sur l'hôte interne : « Compte local », rôle admin
  state.user = 'admin@local'; state.host = INT; await open(base + '/');
  await until(`(document.getElementById('who')||{}).textContent?.includes('admin@local')`);
  await evalJs(`document.getElementById('who').click()`);
  const lm = await until(`!!document.querySelector('#ctxmenu.whomenu')`) ? await evalJs(`document.querySelector('#ctxmenu.whomenu').textContent`) : '';
  check('admin@local sur l\'hôte interne : « Compte local », administrateur', lm.includes('Compte local') && lm.includes('administrateur'), lm);

  // 4. admin@local sur l'hôte externe : refusé par le launcher (403, user)
  state.host = EXT; r = await fetch(base + '/', { redirect: 'manual' });
  check('admin@local sur l\'hôte externe : 403', r.status === 403 && r.headers.get('x-launcher-refusal') === 'user', `HTTP ${r.status}`);

  // 5. session expirée : le proxy répond 401 à /api/* ; modale bloquante « Se reconnecter », terminaux figés
  state.user = 'ccoupel'; state.host = EXT; state.expired = false; await open(base + '/');
  await until(`(document.getElementById('who')||{}).textContent?.includes('ccoupel')`);
  state.expired = true;
  await evalJs(`document.getElementById('cfgbtn').click()`);
  check('session expirée : modale « Se reconnecter »', await until(`!!document.getElementById('expired') && document.getElementById('expired').textContent.includes('Se reconnecter')`, 25000));
  const blocked = await evalJs(`(() => { let n = 0; document.addEventListener('keydown', () => n++); document.body.dispatchEvent(new KeyboardEvent('keydown', {key:'a', bubbles:true, cancelable:true})); return n; })()`);
  check('session expirée : frappes bloquées', blocked === 0, `reçues : ${blocked}`);
  state.expired = false;
  await evalJs(`[...document.querySelectorAll('#expired button')].find(b => b.textContent.includes('Se reconnecter')).click()`);
  check('« Se reconnecter » recharge /', await until(`location.pathname === '/' && !document.getElementById('expired')`, 10000));
} catch (e) {
  console.log('ERREUR harnais : ' + (e && e.stack || e)); process.exitCode = 2;
}
chrome.kill(); proxy.close(); await sleep(200);
const bad = results.filter(([, ok]) => !ok).length;
console.log(`${results.length - bad}/${results.length} verts`);
process.exit(process.exitCode === 2 ? 2 : bad ? 1 : 0);
