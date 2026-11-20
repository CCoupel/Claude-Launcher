// Simulation (node) de #215 (amendement) : résumé de l'approbation d'environnement, échappement. Usage : node env_trust.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(sig) {
  const at = src.indexOf(sig); if (at < 0) { console.log('KO : ' + sig + ' absente'); process.exit(1); }
  const open = src.indexOf('{', src.indexOf(')', at)); let d = 0;
  for (let k = open; k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) return src.slice(at, k + 1); }
}
const tn = src.indexOf('const trustNote = '); const tnEnd = src.indexOf('\nasync function envTrustBox');
const ctx = {}; vm.createContext(ctx);
vm.runInContext(grab('function safeText(') + '\n' + grab('function trustRows(') + '\n' + src.slice(tn, tnEnd) + '\nglobalThis.t = { safeText, trustRows, trustNote };', ctx);
const { safeText, trustRows, trustNote } = ctx.t;
let ko = 0; const eq = (n, a, b) => { if (JSON.stringify(a) !== JSON.stringify(b)) { ko++; console.log('KO', n, JSON.stringify(a), '!=', JSON.stringify(b)); } };
eq('esc terminal', safeText('a\u001b[31mb'), 'a␛[31mb');
eq('contrôles', safeText('x\u0007y\u0085z'), 'x·y·z');
eq('texte gardé', safeText('curl -fsSL https://x | bash\n\tok <b>'), 'curl -fsSL https://x | bash\n\tok <b>');
eq('bidi', safeText('a‮b'), 'a·b');
eq('null', safeText(null), '');
for (const c of ['\u200e', '\u200f', '\u061c', '\u200b', '\u200c', '\u200d', '\ufeff']) eq('invisible ' + c.charCodeAt(0).toString(16), safeText('a' + c + 'b'), 'a\u00b7b');
eq('vide', trustRows({ spec: {} }), []);
eq('vide 2', trustRows(null), []);
const info = { chosenImage: 'debian:12', spec: { image: 'ghcr.io/x:1', packages: ['kubectl', 'jq'], packagesByManager: { apt: ['git'] }, setup: ['curl x | bash', 'pip i'], onStart: ['./up.sh'], mounts: [{ path: '/var/run/docker.sock', ro: false }, { path: '/a', ro: true }] }, risk: { exec: 3, hostMounts: 1 } };
const rows = trustRows(info); const by = Object.fromEntries(rows.map((r) => [r.label, r]));
eq('image', by.Image.items, ['ghcr.io/x:1 (au lieu de debian:12 choisie)']);
eq('setup warn', [by.setup.tone, by.setup.items.length], ['warn', 2]);
eq('montages', by.Montages.items, ['/var/run/docker.sock (écriture)', '/a (lecture)']); eq('montages err', by.Montages.tone, 'err');
eq('pbm', by['Paquets par gestionnaire'].items, ['apt : git']);
eq('même image', trustRows({ chosenImage: 'a', spec: { image: 'a' } })[0].items, ['a']);
eq('échappement dans les lignes', trustRows({ spec: { setup: ['x\u001b[2Jy'] } })[0].items, ['x␛[2Jy']);
eq('note', trustNote(info), '3 commandes exécutées dans le conteneur · 1 montage hors du projet');
eq('note vide', trustNote({}), '');
if (/innerHTML/.test(src.slice(src.indexOf('async function envTrustBox'), src.indexOf('// Proposition /init-project (évènement')))) { ko++; console.log('KO innerHTML dans envTrustBox'); }
console.log(ko ? ko + ' KO' : 'OK'); process.exit(ko ? 1 : 0);
