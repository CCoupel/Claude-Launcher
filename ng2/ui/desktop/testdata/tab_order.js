// Simulation (node) de l'ordre des onglets (#191) : fonctions pures dropIndex (point de dépôt).
// Usage : node tab_order.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const grab = (n) => { const a = src.indexOf('function ' + n + '('); if (a < 0) { console.log('KO : ' + n + ' absente'); process.exit(1); }
  let d = 0, e = -1; for (let k = src.indexOf('{', a); k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) { e = k + 1; break; } } return src.slice(a, e); };
const ctx = {}; vm.createContext(ctx);
vm.runInContext(['dropIndex'].map(grab).join('\n') + '\nglobalThis.f = { dropIndex };', ctx);
const { dropIndex } = ctx.f;
let ko = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { console.log(`KO ${name} : ${g}, attendu ${w}`); ko++; } else console.log('OK ' + name); };
eq('dropIndex début', dropIndex([10, 30, 50], 5), 0);
eq('dropIndex milieu', dropIndex([10, 30, 50], 35), 2);
eq('dropIndex fin', dropIndex([10, 30, 50], 99), 3);
eq('dropIndex sans onglet', dropIndex([], 10), 0);
process.exit(ko ? 1 : 0);
