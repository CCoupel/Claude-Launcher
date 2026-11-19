// Simulation (node) de la logique client de #198 : que faire quand le flux se reconnecte et que `server` a changé.
// Usage : node server_version.js app.js
// Contrat (contracts/web-server.md « Version du serveur ») : le client garde le `server` du premier init.
//   version différente → « update » (bandeau Recharger) ; même version, boot différent → « restart »
//   (reconnexion des terminaux, bandeau retiré à la fin) ; sinon « none ».
// Le client expose cette décision en fonction pure `serverVersionAction(prev, next)` de app.js.
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
const at = src.indexOf('function serverVersionAction(');
if (at < 0) { console.log('KO : function serverVersionAction(prev, next) absente de app.js'); process.exit(1); }
let i = src.indexOf('{', at), depth = 0, end = -1;
for (let k = i; k < src.length; k++) {
  if (src[k] === '{') depth++;
  else if (src[k] === '}' && --depth === 0) { end = k + 1; break; }
}
const ctx = {};
vm.createContext(ctx);
vm.runInContext(src.slice(at, end) + '\nglobalThis.f = serverVersionAction;', ctx);
const f = ctx.f;
const S = (version, boot) => ({ version, boot });
const cases = [
  ['premier init (rien de gardé)', undefined, S('v6.4.0', 'a'), 'none'],
  ['même version, même boot', S('v6.4.0', 'a'), S('v6.4.0', 'a'), 'none'],
  ['même version, autre boot : redémarrage simple', S('v6.4.0', 'a'), S('v6.4.0', 'b'), 'restart'],
  ['version différente, même boot', S('v6.4.0', 'a'), S('v6.4.1', 'a'), 'update'],
  ['version différente, autre boot : mise à jour', S('v6.4.0', 'a'), S('v6.4.1', 'b'), 'update'],
  ['4e champ de la version', S('v6.4.0.3', 'a'), S('v6.4.0.4', 'b'), 'update'],
  ['serveur ancien sans `server` après reconnexion', S('v6.4.0', 'a'), undefined, 'none'],
  ['serveur ancien puis serveur récent', undefined, S('v6.4.0', 'a'), 'none'],
  ['aucun des deux', undefined, undefined, 'none'],
];
let ko = 0;
for (const [name, prev, next, want] of cases) {
  const got = f(prev, next);
  if (got !== want) { console.log(`KO ${name} : ${got}, attendu ${want}`); ko++; } else console.log('OK ' + name);
}
process.exit(ko ? 1 : 0);
