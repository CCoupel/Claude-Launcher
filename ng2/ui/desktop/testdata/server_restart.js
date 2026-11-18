// Simulation (node) : après un redémarrage du serveur (#198), les vues ouvertes sont gardées, resynchronisées quand
// leur onglet revient, et celles qui ne reviennent pas sont signalées (jamais retirées en silence).
// Usage : node server_restart.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(name) {
  const at = src.indexOf('function ' + name + '(');
  if (at < 0) { console.log('KO : function ' + name + ' absente'); process.exit(1); }
  let d = 0, end = -1;
  for (let k = src.indexOf('{', at); k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) { end = k + 1; break; } }
  return src.slice(at, end);
}
const code = ['serverVersionAction', 'reattachPlan', 'graceStep', 'graceEnd', 'graceFallback', 'graceRefuse', 'refusedSync', 'onServerInfo'].map(grab).join('\n');
const log = { sync: [], drop: [], banner: [] };
const ctx = {
  S: { views: { '/a': {}, '/b': {} }, view: '/b', snap: { tabs: [] }, server: { version: 'v6.4.0', boot: 'x' } },
  REATTACH_MS: 80, REATTACH_FALLBACK_MS: 20, leaders: { tiles: {} }, verTxt: (v) => v,
  transport: { call: (m, a) => { log.sync.push(m + ':' + a.path); return Promise.resolve({ ok: true }); } },
  dropView: (p) => { log.drop.push(p); delete ctx.S.views[p]; },
  srvBanner: (k, t, b, o) => { log.banner.push(k + '|' + t); log.ok = o; }, setTimeout, clearTimeout,
};
vm.createContext(ctx);
vm.runInContext(code + '\nglobalThis.api = { refusedSync, onServerInfo, graceStep, graceEnd, graceFallback };', ctx);
const fail = (m) => { console.log('KO ' + m); process.exit(1); };
(async () => {
  ctx.api.onServerInfo({ version: 'v6.4.0', boot: 'y' });                // redémarrage
  if (!ctx.S.grace || ctx.S.grace.pending.size !== 2) fail('redémarrage : vues non mémorisées');
  if (!/Serveur redémarré/.test(log.banner[0]) ) fail('bandeau orange absent');
  ctx.S.snap = { tabs: [{ path: '/a' }] }; ctx.api.graceStep();            // le serveur a rattaché /a seulement
  if (log.sync.join() !== 'termSync:/a') fail('termSync de /a attendu : ' + log.sync);
  if (log.drop.length) fail('vue retirée trop tôt : ' + log.drop);
  await new Promise((r) => setTimeout(r, 40));                             // repli : /b demande un rattachement, une fois
  if (log.sync.join() !== 'termSync:/a,reattach:/b') fail('repli reattach de /b attendu : ' + log.sync);
  ctx.S.snap = { tabs: [{ path: '/a', state: 'ready' }, { path: '/b', state: 'opening' }] }; ctx.api.graceStep();
  if (log.sync.includes('termSync:/b')) fail('termSync avant « ready »');
  await new Promise((r) => setTimeout(r, 80));                             // délai écoulé : /b n'est pas revenue
  if (log.drop.join() !== '/b' || ctx.S.view !== 'leaders') fail('/b aurait dû être retirée et signalée : ' + log.drop);
  if (!/^orange\|1 terminal non rattaché — recharger$/.test(log.banner[log.banner.length - 1])) fail('bandeau « non rattaché » absent : ' + log.banner);
  // tout revient : bandeau retiré sans « non rattaché »
  log.banner.length = 0; log.sync.length = 0; ctx.S.views = { '/a': {} }; ctx.S.snap = { tabs: [] };
  ctx.api.onServerInfo({ version: 'v6.4.0', boot: 'z' });
  ctx.S.snap = { tabs: [{ path: '/a' }] }; ctx.api.graceStep();
  if (log.banner[log.banner.length - 1] !== 'orange|' && log.banner[log.banner.length - 1] !== '|') fail('bandeau non retiré : ' + log.banner);
  // un bandeau « Recharger » apparu entre-temps n'est pas effacé
  log.banner.length = 0; ctx.S.views = { '/a': {} }; ctx.S.snap = { tabs: [] };
  ctx.api.onServerInfo({ version: 'v6.4.0', boot: 'w' });
  ctx.api.onServerInfo({ version: 'v6.5.0', boot: 'w2' });                 // mise à jour : nouveau bandeau bleu
  const before = log.banner.length; ctx.S.snap = { tabs: [{ path: '/a' }] }; ctx.api.graceStep();
  if (log.banner.length !== before) fail('le bandeau bleu a été touché : ' + log.banner.slice(before));
  // reattach {ok:false} : message clair pour ce projet, vue retirée, pas d'abandon silencieux
  log.banner.length = 0; log.drop.length = 0; ctx.S.views = { '/p/projet-x': {} }; ctx.S.snap = { tabs: [] }; ctx.S.view = '/p/projet-x';
  ctx.transport.call = (m, a) => Promise.resolve(m === 'reattach' ? { ok: false } : {});
  ctx.api.onServerInfo({ version: 'v6.4.0', boot: 'q' });
  ctx.api.graceFallback();
  await new Promise((r) => setTimeout(r, 10));
  if (log.drop.join() !== '/p/projet-x') fail('vue refusée non retirée : ' + log.drop);
  if (!/session introuvable — rouvrir le projet : projet-x/.test(log.banner[log.banner.length - 1] || '')) fail('message « session introuvable » absent : ' + log.banner);
  if (log.ok !== true) fail('bouton OK attendu sur le bandeau « session introuvable »');
  // le projet est rouvert : bandeau retiré ; un autre bandeau du serveur n'est pas effacé
  const nb = log.banner.length; ctx.api.refusedSync([{ path: '/autre' }]);
  if (log.banner.length !== nb) fail('bandeau retiré pour un autre projet');
  ctx.api.refusedSync([{ path: '/p/projet-x' }]);
  if (log.banner[log.banner.length - 1] !== '|') fail('bandeau non retiré à la réouverture : ' + log.banner);
  console.log('OK'); process.exit(0);
})();
