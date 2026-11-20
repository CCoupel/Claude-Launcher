// Simulation (node) du bandeau #219 : `launcherBarState(l)` de app.js. Usage : node launcher_bar.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const at = src.indexOf('function launcherBarState(');
if (at < 0) { console.log('KO : launcherBarState absente'); process.exit(1); }
let depth = 0, end = -1;
for (let k = src.indexOf('{', at); k < src.length; k++) { if (src[k] === '{') depth++; else if (src[k] === '}' && --depth === 0) { end = k + 1; break; } }
const ctx = {}; vm.createContext(ctx); vm.runInContext(src.slice(at, end) + '\nglobalThis.f = launcherBarState;', ctx);
const cases = [
  ['absent', undefined, null], ['rien', { binChanged: false, stale: 0 }, null],
  ['binaire changé', { binChanged: true, stale: 0 }, 'g'], ['changé + 2 anciens', { binChanged: true, stale: 2 }, 'g'],
  ['anciens seulement', { stale: 3 }, 'y'],
];
let ko = 0;
for (const [n, l, k] of cases) { const r = ctx.f(l); const got = r ? r.kind : null; if (got !== k) { ko++; console.log('KO', n, got, '!=', k); } }
if (cases[3] && !/2 processus anciens/.test(ctx.f({ binChanged: true, stale: 2 }).text)) { ko++; console.log('KO pluriel'); }
console.log(ko ? ko + ' KO' : 'OK ' + cases.length + ' cas'); process.exit(ko ? 1 : 0);
