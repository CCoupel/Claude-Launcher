// Simulation (node) de #216 : « ce qui change », même emplacement, messages d'échec. Usage : node project_move.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(sig) {
  const at = src.indexOf(sig); if (at < 0) { console.log('KO : ' + sig + ' absente'); process.exit(1); }
  const open = src.indexOf('{', src.indexOf(')', at)); let d = 0;
  for (let k = open; k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) return src.slice(at, k + 1); }
}
const jn = src.slice(src.indexOf('const joinNewPath = '), src.indexOf('// Chargeur de catégories'));
const sl = src.slice(src.indexOf('const sameLocation = '), src.indexOf('\n', src.indexOf('const sameLocation = ')));
const ctx = {}; vm.createContext(ctx);
vm.runInContext(jn + '\n' + sl + '\n' + grab('function moveRows(') + '\n' + grab('function moveFailText(') + '\nglobalThis.t = { sameLocation, moveRows, moveFailText };', ctx);
const { sameLocation, moveRows, moveFailText } = ctx.t;
let ko = 0; const eq = (n, a, b) => { if (JSON.stringify(a) !== JSON.stringify(b)) { ko++; console.log('KO', n, JSON.stringify(a), '!=', JSON.stringify(b)); } };
const info = { base: '/a', category: 'INFRA', name: 'x', key: 'infra-x' };
eq('même', sameLocation(info, '/a', 'INFRA', 'x'), true);
eq('même, catégorie undefined', sameLocation({ base: '/a', name: 'x' }, '/a', '', 'x'), true);
eq('autre nom', sameLocation(info, '/a', 'INFRA', 'y'), false);
eq('autre base', sameLocation(info, '/b', 'INFRA', 'x'), false);
let r = moveRows(info, '/b', 'PRO', 'x');
eq('dossier', [r[0].old, r[0].text], ['/a/INFRA/x', '/b/PRO/x']);
eq('nom recalculé', r[1].dim, '(recalculé selon la catégorie et le nom)');
eq('nom inchangé', moveRows(info, '/b', 'INFRA', 'x')[1].dim, '(inchangé)');
eq('trust gardée', r.some((x) => x.label === 'Approbation env.'), true);
eq('échec simple', moveFailText({ msg: 'destination existante' }), 'destination existante');
eq('échec étape annulé', moveFailText({ step: 'ctr', msg: 'boom', rolledBack: true }), 'étape « ctr » : boom — retour arrière effectué, projet inchangé.');
eq('partiel', moveFailText({ state: 'partial', done: ['dir', 'assign'], msg: 'x' }), 'x — retour arrière incomplet (étapes faites : dir, assign) : l\'état du projet est inachevé.');
eq('nul', moveFailText(null), 'déplacement refusé');
console.log(ko ? ko + ' KO' : 'OK'); process.exit(ko ? 1 : 0);
