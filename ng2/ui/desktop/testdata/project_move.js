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
vm.runInContext(jn + '\n' + sl + '\n' + grab('function moveRows(') + '\n' + grab('function moveFailText(') + '\n' + grab('function moveMenuState(') + '\nglobalThis.t = { sameLocation, moveRows, moveFailText, moveMenuState };', ctx);
const { sameLocation, moveRows, moveFailText, moveMenuState } = ctx.t;
let ko = 0; const eq = (n, a, b) => { if (JSON.stringify(a) !== JSON.stringify(b)) { ko++; console.log('KO', n, JSON.stringify(a), '!=', JSON.stringify(b)); } };
const info = { base: '/a', category: 'INFRA', name: 'x', key: 'infra-x' };
eq('même', sameLocation(info, '/a', 'INFRA', 'x'), true);
eq('même, catégorie undefined', sameLocation({ base: '/a', name: 'x' }, '/a', '', 'x'), true);
eq('autre nom', sameLocation(info, '/a', 'INFRA', 'y'), false);
eq('autre base', sameLocation(info, '/b', 'INFRA', 'x'), false);
const dest = { ok: true, key: 'pro-x', keyChanged: true, crossDevice: true, impacts: [{ what: 'dir', from: '/a/INFRA/x', to: '/b/PRO/x' }, { what: 'ctr', from: 'infra-x', to: '' }, { what: 'assign', value: 'srv1' }, { what: 'conf', keys: ['PROJECT_COLORS'] }, { what: 'history', from: 'a', to: 'b' }] };
const r = moveRows(info, dest);
eq('libellés', r.map((x) => x.label), ['Dossier', 'Nom', 'Conteneur / pod', 'projects.json', 'config.conf', 'Historique claude']);
eq('dossier', [r[0].old, r[0].text], ['/a/INFRA/x', '/b/PRO/x']);
eq('nom recalculé', [r[1].text, r[1].dim], ['pro-x', '(recalculé : était infra-x)']);
eq('nom inchangé', moveRows(info, { ok: true, key: 'infra-x', keyChanged: false, impacts: [] })[0].dim, '(inchangé)');
eq('conf absent = pas de ligne', moveRows(info, { ok: true, impacts: [{ what: 'dir', from: 'a', to: 'b' }] }).some((x) => x.label === 'config.conf'), false);
eq('trust', moveRows(info, { ok: true, impacts: [{ what: 'trust', label: 'Approbation de l\'environnement : conservée' }] }).some((x) => x.label === 'Approbation' && x.text === 'Approbation de l\'environnement : conservée'), true);
eq('dest refusée', moveRows(info, { ok: false, exists: true }), []);
eq('sans dest', moveRows(info, null), []);
eq('échec simple', moveFailText({ msg: 'destination existante' }), 'destination existante');
eq('échec étape annulé', moveFailText({ step: 'ctr', msg: 'boom', rolledBack: true }), 'étape « ctr » : boom — retour arrière effectué, projet inchangé.');
eq('partiel', moveFailText({ state: 'partial', done: ['dir', 'assign'], msg: 'x' }), 'x — retour arrière incomplet (étapes faites : dir, assign) : l\'état du projet est inachevé.');
eq('nul', moveFailText(null), 'déplacement refusé');
eq('menu ouvert', moveMenuState({ open: true }), { disabled: true, why: 'Fermez le projet pour le déplacer ou le renommer' });
eq('menu fermé', moveMenuState({ open: false }), { disabled: false, why: '' });
eq('menu sans ligne', moveMenuState(undefined), { disabled: false, why: '' });
console.log(ko ? ko + ' KO' : 'OK'); process.exit(ko ? 1 : 0);
