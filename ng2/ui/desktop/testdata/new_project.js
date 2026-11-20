// Simulation (node) de #214 : pré-sélection base/catégorie, chemin final, rechargement des catégories.
// Usage : node new_project.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(sig) {
  const at = src.indexOf(sig); if (at < 0) { console.log('KO : ' + sig + ' absente'); process.exit(1); }
  const open = src.indexOf('{', src.indexOf(')', at)); let d = 0;
  for (let k = open; k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) return src.slice(at, k + 1); }
}
const ctx = {}; vm.createContext(ctx);
vm.runInContext(grab('function splitPreselect(') + '\n' + grab('function makeCatLoader(') +
  '\nconst joinNewPath = ' + src.slice(src.indexOf('const joinNewPath = ') + 20, src.indexOf('// Chargeur de catégories')) +
  '\nconst categoryError = ' + src.slice(src.indexOf('const categoryError = ') + 22, src.indexOf('\n', src.indexOf('const categoryError = ')))+ '\nglobalThis.t = { splitPreselect, makeCatLoader, joinNewPath, categoryError };', ctx);
const { splitPreselect, makeCatLoader, joinNewPath, categoryError } = ctx.t;
let ko = 0; const eq = (n, a, b) => { if (JSON.stringify(a) !== JSON.stringify(b)) { ko++; console.log('KO', n, JSON.stringify(a), '!=', JSON.stringify(b)); } };
const B = ['/srv/projets', '/srv/projets/sub', '/data/P'];
eq('vide', splitPreselect(B, ''), { base: '', category: '' });
eq('base', splitPreselect(B, '/data/P'), { base: '/data/P', category: '' });
eq('catégorie', splitPreselect(B, '/data/P/INFRA/x'), { base: '/data/P', category: 'INFRA/x' });
eq('base la plus longue', splitPreselect(B, '/srv/projets/sub/A'), { base: '/srv/projets/sub', category: 'A' });
eq('préfixe voisin', splitPreselect(B, '/srv/projetsX/A'), { base: '', category: '' });
eq('slash final', splitPreselect(B, '/data/P/INFRA/'), { base: '/data/P', category: 'INFRA' });
eq('chemin', joinNewPath('/data/P/', 'INFRA', 'x'), '/data/P/INFRA/x');
eq('sans catégorie', joinNewPath('/data/P', '', 'x'), '/data/P/x');
eq('windows', joinNewPath('D:\\P', 'A', 'x'), 'D:\\P\\A\\x');
eq('nouvelle sans nom', !!categoryError(true, '  '), true);
eq('nouvelle avec nom', categoryError(true, 'A'), '');
eq('existante vide', categoryError(false, ''), '');
(async () => {
  // le dernier appel gagne : la réponse lente de la base A arrive après celle de B et est ignorée
  const applied = []; let resA;
  const load = makeCatLoader((b) => b === 'A' ? new Promise((r) => { resA = r; }) : Promise.resolve({ categories: [{ path: 'Z', projects: 1 }] }), (b, c) => applied.push([b, c.map((x) => x.path)]));
  const pA = load('A'); const pB = load('B');
  eq('B appliquée', await pB, true);
  resA({ categories: [{ path: 'OLD', projects: 0 }] });
  eq('A ignorée', await pA, false);
  eq('appliqué', applied, [['B', ['Z']]]);
  const l2 = makeCatLoader(() => Promise.reject(new Error('x')), (b, c) => applied.push([b, c.length]));
  await l2('C'); eq('erreur = liste vide', applied[1], ['C', 0]);
  console.log(ko ? ko + ' KO' : 'OK'); process.exit(ko ? 1 : 0);
})();
