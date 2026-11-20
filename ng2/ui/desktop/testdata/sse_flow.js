// Simulation (node) de startFlow (#171, #198) : coupure du flux, init qui échoue (502) puis flux rouvert →
// init rejoué avant la réouverture, bandeau « connexion perdue » affiché puis retiré, erreurs d'applyInit tracées.
// Usage : node sse_flow.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const at = src.indexOf('function startFlow(');
if (at < 0) { console.log('KO : startFlow absente'); process.exit(1); }
let d = 0, end = -1;
for (let k = src.indexOf('{', at); k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) { end = k + 1; break; } }
const ctx = { console: { warn() {}, error() {}, info() {} } };
vm.createContext(ctx);
vm.runInContext(src.slice(at, end) + '\nglobalThis.startFlow = startFlow;', ctx);
// Les natifs du navigateur (setTimeout, fetch…) lèvent « Illegal invocation » quand ils sont appelés comme méthode d'un
// autre objet (this ≠ window) : node, lui, l'accepte. nat() reproduit ce refus ; tout env.* natif doit être lié.
const nat = (f) => function (...a) { if (this !== undefined && this !== globalThis) throw new TypeError('Illegal invocation'); return f(...a); };
const log = [], sources = [];
class ES { constructor(u) { this.u = u; sources.push(this); log.push('open'); } close() { log.push('close'); } }
const timers = [], watches = [];
let initFails = 2, lost = false, throwApply = true;
const env = {
  watch: (f) => { const h = { f }; watches.push(h); return h; }, unwatch: (h) => { if (h) h.off = true; }, newES: (u) => new ES(u), race: (p) => p, cb() {}, lost: () => lost,
  setTimeout: (f, ms) => { timers.push([f, ms]); },
  init: async () => { log.push('init'); if (initFails-- > 0) throw new Error('502'); return { ok: 1 }; },
  applyInit: () => { log.push('apply'); if (throwApply) { throwApply = false; throw new Error('boom'); } },
  banner: (on) => log.push(on ? 'banner+' : 'banner-'),
};
const fail = (m) => { console.log('KO ' + m + ' : ' + log.join(',')); process.exit(1); };
const tick = async () => { const [f] = timers.shift(); await f(); };
(async () => {
  ctx.startFlow(env);
  if (log.join() !== 'open') fail('ouverture initiale sans init attendue');
  sources[0].onerror();                                  // coupure (pod supprimé)
  if (!log.includes('banner+')) fail('bandeau « connexion perdue » absent');
  await tick();                                          // init échoue (502) : flux NON rouvert
  if (log.filter((x) => x === 'open').length !== 1) fail('flux rouvert alors qu\'init a échoué');
  if (timers[0][1] !== 3000) fail('backoff non doublé : ' + timers[0][1]);
  await tick();                                          // init échoue encore
  await tick();                                          // init réussi (applyInit lève : tracé, flux rouvert quand même)
  if (log.filter((x) => x === 'open').length !== 2) fail('flux non rouvert après init réussi');
  if (log.lastIndexOf('init') > log.lastIndexOf('open')) fail('init doit précéder la réouverture');
  if (!log.includes('banner-')) fail('bandeau non retiré dès qu\'init réussit');
  // reconnexion native (même EventSource, sans nouvel open) : onopen / message retirent le bandeau même si init n'a pas été rejoué
  const pre = sources.length; env.banner = (on) => log.push(on ? 'banner+' : 'banner-');
  sources[1].onerror(); if (log[log.length - 1] !== 'banner+') fail('bandeau attendu après la 2e coupure');
  sources[1].onopen();
  if (log[log.length - 1] !== 'banner-' || sources.length !== pre) fail('bandeau non retiré par onopen sans nouvel EventSource');
  sources[1].onerror(); sources[1].onmessage({ data: '{"name":"hello","data":{}}' });
  if (log[log.length - 1] !== 'banner-') fail('bandeau non retiré par un message du flux');
  // flux muet (ni ping ni évènement, aucun onerror) : le watchdog le ferme, bandeau, puis init et réouverture
  initFails = 0; await tick(); log.length = 0;     // reprise normale : un flux vivant, watchdog armé
  const live = watches.filter((w) => !w.off).pop();
  if (!live) fail('watchdog non armé sur le flux ouvert');
  const opens = sources.length;
  live.f();                                             // 40 s sans évènement
  if (log[0] !== 'close' || !log.includes('banner+')) fail('flux muet non fermé / bandeau absent');
  await tick();                                         // init rejoué
  if (sources.length !== opens + 1 || !log.includes('init')) fail('init + réouverture attendus après le watchdog');
  const wasOff = watches.slice(0, -1).filter((w) => !w.off).length;
  sources[sources.length - 1].onmessage({ data: '{"name":"ping","data":{}}' });
  if (log[log.length - 1] !== 'banner-' && !log.includes('banner-')) fail('bandeau non retiré au premier signe de vie');
  lost = true; const n = timers.length; sources[1].onerror();   // session perdue : on arrête
  if (timers.length !== n) fail('nouvel essai malgré session perdue');
  // évènement « hello » : même décision de version qu'init, un seul bandeau
  const grab = (n) => { const a = src.indexOf('function ' + n + '('); let dd = 0, e = -1; for (let k = src.indexOf('{', a); k < src.length; k++) { if (src[k] === '{') dd++; else if (src[k] === '}' && --dd === 0) { e = k + 1; break; } } return src.slice(a, e); };
  const banners = [];
  const c2 = { S: { views: {}, server: { version: 'v6.4.0', boot: 'a' } }, console: { info() {} }, verTxt: (v) => v, srvBanner: (k, t) => banners.push(k + '|' + t), leaders: { tiles: {} }, setTimeout: () => 0, clearTimeout() {}, transport: { call: () => Promise.resolve({}) }, REATTACH_MS: 1, REATTACH_FALLBACK_MS: 1 };
  vm.createContext(c2);
  vm.runInContext(['serverVersionAction', 'onServerInfo', 'onEvent'].map(grab).join('\n') + '\nglobalThis.onEvent = onEvent;', c2);
  c2.onEvent('hello', { server: { version: 'v6.4.0', boot: 'a' } });
  if (banners.length) fail('hello identique : aucun bandeau attendu : ' + banners);
  c2.onEvent('hello', { server: { version: 'v6.5.0', boot: 'b' } });
  if (banners.length !== 1 || !/^blue\|.*Serveur mis à jour \(v6\.5\.0\)/.test(banners[0])) fail('hello : bandeau « mis à jour » attendu : ' + banners);
  c2.onEvent('hello', { server: { version: 'v6.5.0', boot: 'b' } });
  if (banners.length !== 2 || banners[1] !== banners[0]) fail('hello répété : même bandeau attendu, pas de double');
  c2.onEvent('hello', undefined);                        // hello sans server : sans effet
  // init OK puis /api/events en échec répétés (503) : backoff croissant, pas de boucle à 1,5 s
  const t2 = [], s2 = []; let race2 = [];
  class ES2 { constructor() { s2.push(this); } close() {} }
  ctx.startFlow({ watch: () => ({}), unwatch() {}, newES: () => new ES2(), race: (p, ms) => { race2.push(ms); return p; }, cb() {}, lost: () => false,
    setTimeout: (f, ms) => { t2.push([f, ms]); }, init: async () => ({ ok: 1 }), applyInit() {}, banner() {} });
  s2[0].onerror();                                       // coupure
  for (let i = 0; i < 3; i++) { await t2.shift()[0](); s2[s2.length - 1].onerror(); }   // init OK, flux 503 aussitôt
  const delays = t2.map((x) => x[1]);
  if (delays.join() !== '12000') fail('backoff attendu 1,5 → 3 → 6 → 12 s (dernier en attente 12000) : ' + delays);
  // init lent : délai d'abandon croissant 8, 16 puis 30 s
  const r3 = [], t3 = [], s3 = []; let slow = 3;
  ctx.startFlow({ watch: () => ({}), unwatch() {}, newES: () => { const e = { close() {} }; s3.push(e); return e; },
    race: (p, ms) => { r3.push(ms); return slow-- > 0 ? Promise.reject(new Error('délai')) : p; }, cb() {}, lost: () => false,
    setTimeout: (f, ms) => { t3.push([f, ms]); }, init: async () => ({}), applyInit() {}, banner() {} });
  s3[0].onerror();
  for (let i = 0; i < 4; i++) await t3.shift()[0]();
  // suite non décroissante 8000, 16000, puis 30000 répété ; plafond 30 s ; jamais moins de 3 mesures
  const okSeq = r3.length >= 3 && r3[0] === 8000 && r3[1] === 16000 && r3.slice(2).every((x) => x === 30000);
  const okMono = r3.every((x, i) => x <= 30000 && (i === 0 || x >= r3[i - 1]));
  if (!okSeq || !okMono) fail('délais d\'init (8000, 16000, puis 30000 répété, plafond 30 s) : ' + r3);
  // environnement réel (flowEnv) avec des natifs stricts : aucun ne doit être appelé comme méthode d'env
  const c4 = { setTimeout: nat((f, ms) => setTimeout(f, ms)), clearTimeout: nat((h) => clearTimeout(h)), EventSource: ES, sessionIsLost: false,
    transport: { call: async () => ({ ok: 1 }) }, applyInit() {}, connBanner() {}, console };
  vm.createContext(c4);
  vm.runInContext(grab('flowEnv') + '\nglobalThis.flowEnv = flowEnv;', c4);
  const e4 = c4.flowEnv(() => {});
  try {
    const h = e4.watch(() => {}, 1); e4.unwatch(h); e4.setTimeout(() => {}, 1); e4.newES('/api/events');
    await e4.race(Promise.resolve(1), 5); await e4.init(); e4.banner(false); e4.lost();
  } catch (err) { fail('natif appelé comme méthode d\'env : ' + err.message); }
  console.log('OK'); process.exit(0);
})();
