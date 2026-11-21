// Simulation (node) du composant « œil » des éléments cachés (#227, maquette A) : createEye / eyesHide / eyesShown de app.js.
// Usage : node secret_eye.js app.js
// Contrat testé (contracts/desktop-dispatch.md « secretReveal », H8) :
//   - masqué par défaut : state().shown=false, value=null — l'appel serveur n'a lieu qu'au clic (toggle)
//   - valeur affichée seulement le temps voulu : re-masquage au clic (2ᵉ toggle), à l'échéance (30 s), par eyesHide()
//     (changement de rubrique, fermeture), et la valeur est EFFACÉE de l'état (value=null)
//   - refus du serveur (ok:false, accès distant, limite) : rien n'est affiché, message rendu, jamais de valeur
//   - réponse reçue APRÈS un masquage / une fermeture : jetée (pas de réaffichage tardif)
//   - jamais de stockage navigateur du composant ; la valeur n'est pas écrite dans un champ éditable (contrôle du source)
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(re, what) {
  const m = re.exec(src);
  if (!m) { console.log('KO : ' + what + ' absent de app.js'); process.exit(1); }
  const at = m.index;
  if (src[src.indexOf('\n', at) - 1] === ';' && !/function/.test(m[0])) return src.slice(at, src.indexOf('\n', at));
  let d = 0, end = -1;
  for (let k = src.indexOf('{', at); k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) { end = k + 1; break; } }
  return src.slice(at, end);
}
const code = [grab(/const EYES = new Set\(\);/, 'EYES'), grab(/function createEye\(/, 'createEye'),
  grab(/const eyesShown = /, 'eyesShown'), grab(/function eyesHide\(/, 'eyesHide')].join('\n');
const ctx = { console, JSON, Promise, Math, Object, Set, Array, setTimeout, clearTimeout, setInterval, clearInterval };
vm.createContext(ctx);
vm.runInContext(code + '\nglobalThis.F = { createEye, eyesShown, eyesHide };', ctx);
const F = ctx.F;
let ko = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { console.log(`KO ${name} : ${g}, attendu ${w}`); ko++; } else console.log('OK ' + name); };
const SECRET = 'ghp_FAKE_TEST_227_jeton';
// horloge simulée : minuteries pilotées à la main
function clock() {
  let now = 0, id = 0; const t = new Map();
  return {
    setT: (f, ms) => { t.set(++id, { f, at: now + ms, rep: 0 }); return id; }, clearT: (i) => t.delete(i),
    setI: (f, ms) => { t.set(++id, { f, at: now + ms, rep: ms }); return id; }, clearI: (i) => t.delete(i),
    advance(ms) {
      const end = now + ms;
      for (;;) {
        let nx = null;
        for (const [i, x] of t) if (x.at <= end && (nx === null || x.at < t.get(nx).at)) nx = i;
        if (nx === null) break;
        const x = t.get(nx); now = x.at; if (x.rep) x.at += x.rep; else t.delete(nx);
        x.f();
      }
      now = end;
    },
  };
}
const flush = () => new Promise((r) => setImmediate(r));
(async () => {
  // 1. masqué par défaut, aucun appel avant le clic
  let calls = 0, ck = clock(), seen = [];
  let eye = F.createEye(async () => { calls++; return { ok: true, value: SECRET }; }, (s) => seen.push(JSON.stringify(s)), ck);
  eq('masqué au départ', [eye.state().shown, eye.state().value, calls], [false, null, 0]);
  eq('aucune valeur dans les états émis avant clic', seen.some((s) => s.includes(SECRET)), false);
  // 2. clic : valeur affichée, minuterie de 30 s
  await eye.toggle(); await flush();
  eq('affiché après le clic', [eye.state().shown, eye.state().value, calls], [true, SECRET, 1]);
  eq('eyesShown (rafraîchissement doux protégé)', F.eyesShown(), true);
  // 3. 2ᵉ clic : re-masqué, valeur effacée
  await eye.toggle();
  eq('2ᵉ clic : masqué, valeur effacée', [eye.state().shown, eye.state().value], [false, null]);
  eq('eyesShown faux après masquage', F.eyesShown(), false);
  // 4. 30 s : re-masquage automatique, pas avant
  await eye.toggle(); await flush();
  ck.advance(29000);
  eq('29 s : encore affiché', eye.state().shown, true);
  eq('compte à rebours', eye.state().left <= 1 && eye.state().left >= 0, true);
  ck.advance(1500);
  eq('30 s : re-masqué, valeur effacée', [eye.state().shown, eye.state().value], [false, null]);
  // 5. changement de rubrique / fermeture : eyesHide masque TOUS les yeux et vide le registre
  const e2 = F.createEye(async () => ({ ok: true, value: SECRET }), () => {}, clock());
  await eye.toggle(); await e2.toggle(); await flush();
  eq('deux yeux affichés', [eye.state().shown, e2.state().shown], [true, true]);
  F.eyesHide();
  eq('eyesHide : tous masqués et effacés', [eye.state().shown, eye.state().value, e2.state().shown, e2.state().value], [false, null, false, null]);
  eq('eyesHide : plus rien d\'affiché', F.eyesShown(), false);
  // 6. refus du serveur : rien d'affiché, message rendu, aucune valeur
  const refus = [{ ok: false, msg: 'indisponible depuis un accès distant' }, { ok: false, msg: 'limite atteinte' }, null, { ok: true }, { ok: true, value: 42 }];
  for (const r of refus) {
    const e = F.createEye(async () => r, () => {}, clock());
    await e.toggle(); await flush();
    eq('refus ' + JSON.stringify(r), [e.state().shown, e.state().value, typeof e.state().msg === 'string' && e.state().msg.length > 0], [false, null, true]);
  }
  const ex = F.createEye(async () => { throw new Error('réseau'); }, () => {}, clock());
  await ex.toggle(); await flush();
  eq('exception : rien affiché, message', [ex.state().shown, ex.state().value, ex.state().msg], [false, null, 'réseau']);
  // 7. réponse tardive après masquage : jetée
  let release; const slow = F.createEye(() => new Promise((res) => { release = res; }), () => {}, clock());
  const p = slow.toggle();
  eq('occupé pendant l\'appel', slow.state().busy, true);
  slow.hide();
  release({ ok: true, value: SECRET }); await p; await flush();
  eq('réponse tardive jetée', [slow.state().shown, slow.state().value], [false, null]);
  // 8. double clic pendant l'appel : un seul appel serveur
  let n = 0, rel2; const dbl = F.createEye(() => { n++; return new Promise((res) => { rel2 = res; }); }, () => {}, clock());
  const a = dbl.toggle(); dbl.toggle(); rel2({ ok: true, value: SECRET }); await a; await flush();
  eq('double clic : un seul appel', n, 1);
  F.eyesHide();
  // 9. source : pas de stockage navigateur dans le composant, valeur jamais dans un champ éditable
  const body = code;
  eq('aucun stockage navigateur dans le composant', /localStorage|sessionStorage|indexedDB|document\.cookie/.test(body), false);
  eq('aucun champ éditable alimenté par le composant', /\.value\s*=/.test(body.replace(/value = (null|r\.value)/g, '')), false);
  console.log(ko ? ko + ' KO' : 'tout OK');
  process.exit(ko ? 1 : 0);
})();
