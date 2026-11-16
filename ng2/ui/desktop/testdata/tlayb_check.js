// Contrôle du bouton de bascule de mise en page (#153) : exécute les VRAIES fonctions de frontend/app.js
// (layoutFetch, paintLayoutButton, toggleLayout, layoutDrop, fitLeadersTab) dans un contexte vm minimal, sans
// navigateur. Lancé par tlayb_js_test.go (node requis) : `node tlayb_check.js <app.js>`.
const fs = require('fs'), vm = require('vm'), assert = require('assert');
const src = fs.readFileSync(process.argv[2], 'utf8');
const a = src.indexOf('const layoutCache'), b = src.indexOf("// Réduction à l'icône (#162)");
const c = src.indexOf('function fitLeadersTab'), d = src.indexOf("window.addEventListener('resize'");
assert(a > 0 && b > a && c > b && d > c, 'repères introuvables dans app.js (layoutCache … Réduction à l\'icône … fitLeadersTab)');
const layoutSrc = src.slice(a, b), fitSrc = src.slice(c, d);

let total = 0, failed = 0;
const t = (name, f) => { total++; try { f(); console.log('PASS', name); } catch (e) { failed++; console.log('FAIL', name, '-', e.message); } };
const tick = () => new Promise((r) => setTimeout(r, 5));

function mkBtn() {
  const e = { cls: new Set(), attrs: {}, title: '', innerHTML: '', onclick: null,
    classList: { toggle(k, on) { on ? e.cls.add(k) : e.cls.delete(k); }, add(k) { e.cls.add(k); }, remove(k) { e.cls.delete(k); }, contains: (k) => e.cls.has(k) },
    setAttribute(k, v) { e.attrs[k] = String(v); }, removeAttribute(k) { delete e.attrs[k]; }, scrollIntoView() {} };
  return e;
}
// contexte : view = onglet actif ; handlers[m](args, n) = réponse (ou rejet) de l'appel m ; horloge pilotable
function ctx(view, handlers) {
  const calls = [], flashes = [], btn = mkBtn(); btn.id = 'tlayb';
  const k = { now: 1000000 };
  const run = (m, args) => { calls.push([m, args]); const h = handlers && handlers[m]; const n = calls.filter((x) => x[0] === m).length; return h ? Promise.resolve().then(() => h(args, n)) : Promise.resolve({ ok: true }); };
  const c = { S: { view }, el: () => btn, $: (id) => (id === 'tlayb' ? btn : null), Promise, parseFloat, Math, Object,
    Date: { now: () => k.now }, getComputedStyle: () => ({}),
    transport: { call: run }, call: run, flash: (m) => flashes.push(m), flashBrief: (m) => flashes.push('brief:' + m) };
  vm.createContext(c); vm.runInContext(layoutSrc, c);
  return { c, calls, flashes, btn, clock: k, n: (m) => calls.filter((x) => x[0] === m).length };
}
const settings = (layout) => ({ projectSettings: () => ({ layout }) });

(async () => {
  // ── structure : le bouton est dans la barre, hors de la zone qui défile ──
  t('renderTabs ajoute layoutButton() après .tscroll', () => assert(/bar\.append\(lt, el\('div', 'tsep'\), scroll, layoutButton\(\)\)/.test(src)));

  // ── onglet Teamleaders : grisé, infobulle, aucun appel, clic ignoré ──
  let x = ctx('leaders', settings('top')); x.c.paintLayoutButton();
  t('Teamleaders : grisé (classe dis, aria-disabled=true) avec infobulle explicative', () => { assert(x.btn.cls.has('dis')); assert.strictEqual(x.btn.attrs['aria-disabled'], 'true'); assert(/Pas de mise en page pour l'onglet Teamleaders/.test(x.btn.title), x.btn.title); });
  await tick(); await x.c.toggleLayout();
  t('Teamleaders : aucun appel (ni projectSettings ni projectApply), clic ignoré', () => assert.strictEqual(x.calls.length, 0));

  // ── projet : lecture, infobulle, bascule ──
  x = ctx('/p/alpha', settings('top')); x.c.paintLayoutButton();
  t('projet, lecture en cours : grisé (aria-disabled=true), infobulle « lecture en cours »', () => { assert.strictEqual(x.btn.attrs['aria-disabled'], 'true'); assert(/lecture en cours/.test(x.btn.title), x.btn.title); });
  await x.c.toggleLayout();
  t('lecture en cours : le clic est ignoré (aucun projectApply)', () => assert.strictEqual(x.n('projectApply'), 0));
  await tick();
  t('projet : lit projectSettings { path }', () => assert.strictEqual(JSON.stringify(x.calls[0]), JSON.stringify(['projectSettings', { path: '/p/alpha' }])));
  t('projet : actif (aria-disabled=false), infobulle « leader en haut — cliquer pour leader à gauche »', () => { assert(!x.btn.cls.has('dis')); assert.strictEqual(x.btn.attrs['aria-disabled'], 'false'); assert(/leader en haut — cliquer pour leader à gauche/.test(x.btn.title), x.btn.title); });
  await x.c.toggleLayout();
  t('clic : projectApply { path, layout: "left" }', () => { const k = x.calls.find((k) => k[0] === 'projectApply'); assert.strictEqual(JSON.stringify(k[1]), JSON.stringify({ path: '/p/alpha', layout: 'left' })); });
  t('clic : icône et infobulle à jour tout de suite (leader à gauche)', () => assert(/leader à gauche — cliquer pour leader en haut/.test(x.btn.title), x.btn.title));
  t('clic : message bref « Mise en page : leader à gauche »', () => assert(x.flashes.some((f) => /Mise en page : leader à gauche/.test(f))));
  await x.c.toggleLayout();
  t('second clic : retour à « top »', () => { const k = x.calls.filter((k) => k[0] === 'projectApply')[1]; assert.strictEqual(k[1].layout, 'top'); });

  // ── refus : retour arrière, message, nouvelle lecture ──
  x = ctx('/p/alpha', { ...settings('top'), projectApply: () => ({ ok: false, msg: 'refusé' }) }); x.c.paintLayoutButton(); await tick();
  await x.c.toggleLayout(); await tick();
  t('refus : message ⚠, cache oublié (nouvelle lecture), bouton revenu à l\'état réel', () => { assert(x.flashes.some((f) => /⚠ refusé/.test(f))); assert(x.n('projectSettings') >= 2, 'pas de relecture'); assert(/leader en haut/.test(x.btn.title)); });
  x = ctx('/p/alpha', { ...settings('top'), projectApply: () => { throw new Error('réseau'); } }); x.c.paintLayoutButton(); await tick();
  await x.c.toggleLayout(); await tick();
  t('appel en échec (exception) : retour arrière, relecture', () => { assert(x.n('projectSettings') >= 2); assert(/leader en haut/.test(x.btn.title)); });

  // ── relecture : au changement d'onglet (forcée), après bascule/enregistrement, espacée sinon ──
  x = ctx('/p/alpha', { projectSettings: (args) => ({ layout: args.path === '/p/beta' ? 'left' : 'top' }) });
  x.c.paintLayoutButton(); await tick();
  for (let i = 0; i < 20; i++) x.c.paintLayoutButton(); // 20 instantanés de suite, même onglet : pas un appel par instantané
  await tick();
  t('même onglet, cache frais : un seul projectSettings malgré 21 rendus', () => assert.strictEqual(x.n('projectSettings'), 1));
  x.c.S.view = '/p/beta'; x.c.paintLayoutButton(); await tick();
  t('changement d\'onglet : relecture forcée du nouveau projet et icône de son organisation', () => { assert.strictEqual(x.n('projectSettings'), 2); assert.strictEqual(x.calls[1][1].path, '/p/beta'); assert(/leader à gauche — cliquer pour leader en haut/.test(x.btn.title), x.btn.title); });
  x.c.S.view = '/p/alpha'; x.c.paintLayoutButton(); await tick();
  t('retour sur un onglet déjà lu (cache frais) : relecture forcée quand même', () => assert.strictEqual(x.n('projectSettings'), 3));
  x.c.layoutDrop('/p/alpha'); x.c.paintLayoutButton(); await tick();
  t('après enregistrement des Réglages / bascule (layoutDrop) : relecture immédiate', () => assert.strictEqual(x.n('projectSettings'), 4));
  x.clock.now += 31000; x.c.paintLayoutButton(); await tick();
  t('même onglet après 30 s : relecture', () => assert.strictEqual(x.n('projectSettings'), 5));
  x.clock.now += 5000; x.c.paintLayoutButton(); await tick();
  t('même onglet moins de 30 s après : pas de relecture', () => assert.strictEqual(x.n('projectSettings'), 5));

  // ── frein sur erreur : attente croissante, jamais un appel par instantané ──
  let fail = true;
  x = ctx('/p/alpha', { projectSettings: () => { if (fail) throw new Error('hors service'); return { layout: 'top' }; } });
  x.c.paintLayoutButton(); await tick();
  t('erreur n°1 : un appel', () => assert.strictEqual(x.n('projectSettings'), 1));
  for (let i = 0; i < 50; i++) x.c.paintLayoutButton(); await tick();
  t('pendant l\'attente (50 rendus) : aucun appel supplémentaire', () => assert.strictEqual(x.n('projectSettings'), 1));
  x.clock.now += 1900; x.c.paintLayoutButton(); await tick();
  t('à +1,9 s : toujours en attente (2 s)', () => assert.strictEqual(x.n('projectSettings'), 1));
  x.clock.now += 200; x.c.paintLayoutButton(); await tick();
  t('à +2,1 s : nouvel essai (erreur n°2)', () => assert.strictEqual(x.n('projectSettings'), 2));
  x.clock.now += 3900; x.c.paintLayoutButton(); await tick();
  t('attente doublée : pas d\'essai à +3,9 s', () => assert.strictEqual(x.n('projectSettings'), 2));
  x.clock.now += 200; x.c.paintLayoutButton(); await tick();
  t('essai à +4,1 s (erreur n°3)', () => assert.strictEqual(x.n('projectSettings'), 3));
  for (let i = 0; i < 10; i++) { x.clock.now += 61000; x.c.paintLayoutButton(); await tick(); }
  const before = x.n('projectSettings');
  x.clock.now += 59000; x.c.paintLayoutButton(); await tick();
  t('plafond de l\'attente : 60 s au plus (pas d\'essai à +59 s, essai au-delà)', () => { assert.strictEqual(x.n('projectSettings'), before); x.clock.now += 2000; x.c.paintLayoutButton(); });
  await tick();
  t('plafond : essai après 61 s', () => assert.strictEqual(x.n('projectSettings'), before + 1));
  fail = false; x.clock.now += 61000; x.c.paintLayoutButton(); await tick();
  t('retour du service : le bouton redevient actif', () => { assert(!x.btn.cls.has('dis')); assert(/leader en haut/.test(x.btn.title)); });
  x = ctx('/p/alpha', { projectSettings: () => ({}) }); x.c.paintLayoutButton(); await tick();
  t('réponse sans layout : traitée comme une erreur (bouton grisé, frein)', () => { assert(x.btn.cls.has('dis')); x.c.paintLayoutButton(); assert.strictEqual(x.n('projectSettings'), 1); });

  // ── #162 : la largeur du bouton compte dans la mesure du débordement ──
  const mk = (lbw) => { const bar = { clientWidth: 1000, querySelector: (s) => ({ '.tl': { offsetWidth: 150, classList: { remove() {}, add() { bar.ico = true; } }, removeAttribute() {}, set title(v) {}, get title() { return ''; } }, '.tscroll': { scrollWidth: 800, querySelector: () => null }, '.tsep': { offsetWidth: 2 }, '.tlayb': lbw === null ? null : { offsetWidth: lbw } }[s] || null) }; return bar; };
  const fit = (lbw) => { const bar = mk(lbw); const k = { $: () => bar, parseFloat, getComputedStyle: () => ({ marginLeft: '0', marginRight: '0' }) }; vm.createContext(k); vm.runInContext(fitSrc, k); k.fitLeadersTab(); return !!bar.ico; };
  t('débordement : 800 ≤ 1000-150-2 sans bouton → pas de réduction', () => assert.strictEqual(fit(null), false));
  t('débordement : bouton de 60 px (848-60=788 < 800) → réduction à l\'icône', () => assert.strictEqual(fit(60), true));
  t('débordement : bouton de 40 px (808 ≥ 800) → pas de réduction (pas d\'oscillation)', () => assert.strictEqual(fit(40), false));

  console.log((total - failed) + '/' + total + ' PASS' + (failed ? ' — ' + failed + ' ÉCHEC(S)' : ''));
  process.exit(failed ? 1 : 0);
})();
