// E2E #254 (addendum 4) — voir run.sh. Reverse proxy à sous-chemin (jamais retiré : le launcher voit /claude-qualif/…), Host et Origin
// réécrits comme le ferait Traefik en https ; TOUTE requête hors de /claude-qualif/ est notée (outside) et refusée : le client web ne
// doit en émettre aucune. Chrome sans tête piloté par CDP (WebSocket natif de node >= 22).
import http from 'node:http';
import { spawn } from 'node:child_process';

const { LPORT, PPORT, CPORT, EXT, INT, CHROME, WORK } = process.env;
const BASE = '/claude-qualif';
const state = { user: 'ccoupel', host: EXT, expired: false };
const outside = [], inside = [];
const results = [];
const check = (name, ok, detail = '') => { results.push([name, ok]); console.log(`${ok ? 'VERT ' : 'ROUGE'} ${name}${ok ? '' : ' — ' + detail}`); };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const proxy = http.createServer((req, res) => {
  const path = req.url || '/';
  if (!(path === BASE || path.startsWith(BASE + '/'))) { outside.push(req.method + ' ' + path); res.writeHead(404); res.end('hors du sous-chemin'); return; }
  inside.push(req.method + ' ' + path);
  if (path.startsWith(BASE + '/oauth2/sign_out')) { res.writeHead(302, { Location: BASE + '/signed-out' }); res.end(); return; }
  const open = (path === BASE + '/signed-out' || path === BASE + '/too-many') && req.method === 'GET';
  if (state.expired && path.startsWith(BASE + '/api/')) { res.writeHead(401); res.end('session expirée'); return; }
  const headers = { ...req.headers, host: state.host };
  delete headers['x-forwarded-user'];
  if (state.user && !open) headers['x-forwarded-user'] = state.user;
  if (headers.origin) headers.origin = 'https://' + state.host;
  headers['x-forwarded-for'] = '192.168.1.20';
  const up = http.request({ host: '127.0.0.1', port: LPORT, path, method: req.method, headers }, (r) => { res.writeHead(r.statusCode, r.headers); r.pipe(res); });
  up.on('error', () => { res.writeHead(502); res.end(); });
  res.on('close', () => up.destroy());
  req.pipe(up);
});
await new Promise((r) => proxy.listen(PPORT, '127.0.0.1', r));
const base = `http://127.0.0.1:${PPORT}`;

const chrome = spawn(CHROME, ['--no-sandbox', '--disable-gpu', `--user-data-dir=${WORK}/chrome`, `--remote-debugging-port=${CPORT}`, '--window-size=1280,900', 'about:blank'], { stdio: 'ignore' });
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
  // 0. serveur : 308 de la base nue, 404 hors base, sans identité 401
  state.user = '';
  let r = await fetch(base + BASE + '/', { redirect: 'manual' });
  check('sans identité sous la base : 401', r.status === 401);
  r = await fetch(base + BASE + '/signed-out');
  check('/signed-out sous la base sans identité : page servie', r.status === 200 && (await r.text()).includes('Se reconnecter'));

  // 1. administrateur : l'interface se charge sous le sous-chemin
  state.user = 'ccoupel'; state.host = EXT; state.expired = false;
  await open(base + BASE + '/');
  check('smoke : interface chargée sous le sous-chemin', await until(`!!document.getElementById('version') && document.getElementById('version').textContent.length > 0`));
  check('<base href> = sous-chemin', (await evalJs(`document.baseURI`)) === base + BASE + '/', String(await evalJs(`document.baseURI`)));
  check('identité affichée sous le sous-chemin', await until(`(document.getElementById('who')||{}).textContent?.includes('ccoupel')`));
  check('flux SSE et appels sous la base', await until(`true`) && (await sleep(1500), inside.some((x) => x.startsWith('GET ' + BASE + '/api/events')) && inside.some((x) => x.startsWith('POST ' + BASE + '/api/call'))), JSON.stringify(inside.slice(0, 12)));
  check('ressources (app.js, style.css) sous la base', inside.some((x) => x === 'GET ' + BASE + '/app.js') || inside.some((x) => x.includes(BASE + '/app.js')), JSON.stringify(inside.slice(0, 12)));

  // 2. menu › déconnexion : sign_out sous la base puis /signed-out sous la base ; « Se reconnecter » revient dans la base
  await evalJs(`document.getElementById('who').click()`);
  await until(`!!document.querySelector('#ctxmenu.whomenu')`);
  await evalJs(`[...document.querySelectorAll('#ctxmenu div')].find(d => d.textContent.includes('Se déconnecter')).click()`);
  check('déconnexion : /claude-qualif/signed-out', await until(`location.pathname === '${BASE}/signed-out'`, 8000));
  check('page signed-out : « Se reconnecter »', (await evalJs(`document.body.textContent`)).includes('Se reconnecter'));
  await evalJs(`[...document.querySelectorAll('a,button')].find(e => e.textContent.includes('Se reconnecter')).click()`);
  check('« Se reconnecter » revient dans le sous-chemin', await until(`location.pathname === '${BASE}/'`, 8000), String(await evalJs(`location.pathname`)));

  // 3. session expirée : modale, « Se reconnecter » = document.baseURI (dans la base, jamais « / »)
  state.user = 'ccoupel'; await open(base + BASE + '/');
  await until(`(document.getElementById('who')||{}).textContent?.includes('ccoupel')`);
  state.expired = true;
  await evalJs(`document.getElementById('cfgbtn').click()`);
  check('session expirée : modale « Se reconnecter »', await until(`!!document.getElementById('expired')`, 25000));
  state.expired = false;
  await evalJs(`[...document.querySelectorAll('#expired button')].find(b => b.textContent.includes('Se reconnecter')).click()`);
  check('modale : « Se reconnecter » reste dans la base', await until(`location.pathname === '${BASE}/' && !document.getElementById('expired')`, 10000), String(await evalJs(`location.pathname`)));

  // 4. rôle user : même interface sous la base
  state.user = 'alice'; await open(base + BASE + '/');
  check('rôle user sous la base : pastille', await until(`(document.getElementById('who')||{}).textContent?.includes('alice')`));

  // 5. AUCUNE requête hors du sous-chemin (le client n'emploie que des chemins relatifs)
  check('aucune requête hors de /claude-qualif/', outside.length === 0, JSON.stringify(outside.slice(0, 8)));
} catch (e) {
  console.log('ERREUR harnais : ' + (e && e.stack || e)); process.exitCode = 2;
}
chrome.kill(); proxy.close(); await sleep(200);
const bad = results.filter(([, ok]) => !ok).length;
console.log(`${results.length - bad}/${results.length} verts`);
process.exit(process.exitCode === 2 ? 2 : bad ? 1 : 0);
