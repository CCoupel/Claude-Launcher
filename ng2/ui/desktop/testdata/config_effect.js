// Simulation (node) de #226 / #227 : pastilles, bulle, rappel d'enregistrement, œil des éléments cachés. Usage : node config_effect.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(sig) {
  const at = src.indexOf(sig); if (at < 0) { console.log('KO : ' + sig + ' absente'); process.exit(1); }
  const open = src.indexOf('{', src.indexOf(')', at)); let d = 0;
  for (let k = open; k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) return src.slice(at, k + 1); }
}
const a = src.indexOf('const EFFECTS = '), b = src.indexOf('function effectBubble(');
const line = (sig) => src.slice(src.indexOf(sig), src.indexOf('\n', src.indexOf(sig)));
const code = src.slice(a, src.indexOf('const effectMeta')) + line('const effectMeta') + '\n' + grab('function effectBubble(') + '\n' + grab('function applyReminder(') + '\nconst EYES = new Set();\n' + grab('function createEye(') + '\n' + line('const eyesShown') + '\n' + grab('function eyesHide(') + '\nglobalThis.t = { effectMeta, effectBubble, applyReminder, createEye, eyesShown, eyesHide, EYES };';
const ctx = { setTimeout, clearTimeout, setInterval, clearInterval, Promise }; vm.createContext(ctx); vm.runInContext(code, ctx);
const { effectMeta, effectBubble, applyReminder, createEye, eyesShown, eyesHide } = ctx.t;
let ko = 0; const eq = (n, x, y) => { if (JSON.stringify(x) !== JSON.stringify(y)) { ko++; console.log('KO', n, JSON.stringify(x), '!=', JSON.stringify(y)); } };
// pastilles
eq('meta claude', effectMeta('claude').label, '✳ sessions claude');
eq('meta absent', effectMeta(undefined), null);
eq('meta inconnu', effectMeta('x'), null);
eq('pas de bulle sans effet', effectBubble({ key: 'K' }, []), null);
const x = { key: 'CLAUDE_DISABLE_MOUSE', label: 'Souris', effect: 'claude', effectHow: { why: 'recopiée', do: 'Rafraîchir', shell: 'F5', action: 'refresh' } };
const ops = [{ name: 'A', path: '/a' }, { name: 'B', path: '/b' }];
const bub = effectBubble(x, ops);
eq('bulle lien', [bub.link.kind, bub.link.text, bub.link.paths], ['refresh', 'Rafraîchir A, B', ['/a', '/b']]);
eq('bulle sans projet ouvert', effectBubble(x, []).link, null);
eq('bulle relance', effectBubble({ label: 'D', effect: 'launcher', effectHow: { why: 'w', action: 'relaunch' } }, []).link.kind, 'relaunch');
eq('bulle reopen sans lien', effectBubble({ effect: 'project', effectHow: { why: 'w', action: 'reopen' } }, ops).link, null);
eq('bulle immédiat', effectBubble({ label: 'L', effect: 'immediate' }, []).why, 'appliqué dès l\'enregistrement');
// rappel
const lab = (k) => ({ CLAUDE_DISABLE_MOUSE: 'Souris', GITHUB_DIR: 'Dossier' }[k] || k);
const ap = { changed: [{ key: 'CLAUDE_DISABLE_MOUSE', effect: 'claude' }, { key: 'GITHUB_DIR', effect: 'launcher' }, { key: 'TEAM_HIDE_IDLE', effect: 'immediate' }], relaunch: true, projects: [{ path: '/a', name: 'A', for: ['claude'] }] };
const rem = applyReminder(ap, lab);
eq('rappel lignes', rem.lines, ['Souris : relancer les sessions claude ouvertes (A) pour l\'appliquer', 'Dossier : relancer le launcher']);
eq('rappel actions', rem.actions.map((y) => y.kind), ['refresh', 'relaunch']);
eq('rappel immédiat seul = rien', applyReminder({ changed: [{ key: 'K', effect: 'immediate' }] }, lab), null);
eq('rappel vide', applyReminder({ changed: [] }, lab), null);
eq('rappel absent', applyReminder(null, lab), null);
// œil : horloge simulée
(async () => {
  const timers = []; let now = 0;
  const fake = { ms: 30000, setT: (f, d) => { timers.push({ f, at: now + d, kind: 't' }); return timers.length; }, clearT: (id) => { if (timers[id - 1]) timers[id - 1].dead = true; }, setI: (f, d) => { timers.push({ f, every: d, next: now + d, kind: 'i' }); return timers.length; }, clearI: (id) => { if (timers[id - 1]) timers[id - 1].dead = true; } };
  const advance = (ms) => { const end = now + ms; for (; now < end; now += 1000) for (const t of timers) { if (t.dead) continue; if (t.kind === 't' && t.at <= now + 1000 && t.at > now) { t.dead = true; t.f(); } if (t.kind === 'i' && t.next <= now + 1000) { t.next += t.every; t.f(); } } now = end; };
  const log = []; let calls = 0;
  const eye = createEye(async () => { calls++; return { ok: true, value: 'ghp_FAKE_TEST_value' }; }, (s) => log.push(s), fake);
  eq('masqué au départ', eye.state().shown, false);
  await eye.toggle();
  eq('révélé', [eye.state().shown, eye.state().value, eye.state().left], [true, 'ghp_FAKE_TEST_value', 30]);
  eq('eyesShown', eyesShown(), true);
  advance(10000);
  eq('compte à rebours', eye.state().left, 20);
  await eye.toggle();
  eq('clic = re-masqué, valeur effacée', [eye.state().shown, eye.state().value], [false, null]);
  eq('un seul appel serveur', calls, 1);
  await eye.toggle(); advance(31000);
  eq('30 s : re-masqué', [eye.state().shown, eye.state().value], [false, null]);
  await eye.toggle(); eyesHide();
  eq('rubrique/fermeture : re-masqué', [eye.state().shown, eye.state().value], [false, null]);
  eq('registre vidé', eyesShown(), false);
  // refus distant
  const eye2 = createEye(async () => ({ ok: false, msg: 'indisponible depuis un accès distant' }), () => {}, fake);
  await eye2.toggle();
  eq('refus : message, aucune valeur', [eye2.state().shown, eye2.state().value, eye2.state().msg], [false, null, 'indisponible depuis un accès distant']);
  const eye3 = createEye(async () => { throw new Error('réseau'); }, () => {}, fake);
  await eye3.toggle(); eq('erreur réseau', [eye3.state().shown, eye3.state().msg], [false, 'réseau']);
  // masqué pendant l'appel : la valeur arrivée après est jetée
  let release; const eye4 = createEye(() => new Promise((r) => { release = r; }), () => {}, fake);
  const p = eye4.toggle(); eye4.hide(); release({ ok: true, value: 'ghp_FAKE_TEST_late' }); await p;
  eq('valeur tardive jetée', [eye4.state().shown, eye4.state().value], [false, null]);
  // jamais de stockage ni de champ éditable
  const eyeSrc = src.slice(src.indexOf('function createEye('), src.indexOf('const eyesShown'));
  if (/localStorage|sessionStorage|store\.set|\.value\s*=/.test(eyeSrc)) { ko++; console.log('KO le composant touche le stockage ou un champ'); }
  const secUi = fs.readFileSync(process.argv[2].replace('app.js', 'settings.js'), 'utf8');
  const sec = secUi.slice(secUi.indexOf("if (x.type === 'secret') {\n    const w = el('span', 'stfont secwrap')"), secUi.indexOf("if (x.type === 'bool') node = stCheck"));
  if (/\.value\s*=\s*(st\.|r\.|v)/.test(sec) || /i\.value = [^']/.test(sec.replace(/i\.value = ''/g, ''))) { ko++; console.log('KO valeur révélée écrite dans un champ'); }
  console.log(ko ? ko + ' KO' : 'OK'); process.exit(ko ? 1 : 0);
})();
