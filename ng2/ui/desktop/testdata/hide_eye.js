// Simulation (node) de l'œil à 3 états du titre de la carte TEAM (#209, maquette v2 team_card__masquage_par_projet_v2.html, proposition A « bouton enfoncé »).
// Usage : node hide_eye.js app.js
// Contrat des fonctions pures exposées par app.js (à confirmer par dev-frontend ; noms ATTENDUS) :
//   hideEyeView(mode, g)       → { mode, open, dim, own, name, tip }
//        mode : 'default' | 'auto' | 'none' ; g = { enabled, delay } (réglage global TEAM_HIDE_IDLE_ENABLED / TEAM_HIDE_IDLE)
//        open : œil OUVERT ssi 'none' ; FERMÉ ssi 'auto' ; 'default' → suit le global (fermé si global activé, ouvert sinon)
//        dim  : grisé ssi 'default' (et seulement lui)
//        own  : choix PROPRE au projet (œil sur fond plein accent, « bouton enfoncé ») ssi 'auto' ou 'none', quel que soit le global ; jamais pour 'default'
//        name : 'default' | 'auto hide' | 'no hide'
//        tip  : « Ce projet : <name> — clic : <suivant> · réglage global : masqués après <delay> s | désactivé »
//   hideEyeNext(mode)          → default → auto → no hide → default (rend 'default' | 'auto' | 'none')
//   hideEyeMenu(mode, g)       → 3 entrées { mode, label, current } dans l'ordre auto, default, none ; current ssi mode courant ;
//                                label 'auto hide — masquer après <delay> s', 'default — réglage global (<…>)', 'no hide — toujours visibles'
//   hideEyeApply(mode)         → argument de projectApply : { hideIdle: mode } ('default' retire l'entrée côté serveur)
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(name) {
  const at = src.indexOf('function ' + name + '(');
  if (at < 0) { console.log('KO : function ' + name + '( absente de app.js'); process.exit(1); }
  let d = 0, end = -1;
  for (let k = src.indexOf('{', at); k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) { end = k + 1; break; } }
  return src.slice(at, end);
}
const names = ['hideEyeView', 'hideEyeNext', 'hideEyeMenu', 'hideEyeApply'];
const ctx = { console, JSON };
vm.createContext(ctx);
vm.runInContext(names.map(grab).join('\n') + '\nglobalThis.F = { ' + names.join(', ') + ' };', ctx);
const F = ctx.F;
let ko = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { console.log(`KO ${name} : ${g}, attendu ${w}`); ko++; } else console.log('OK ' + name); };
const on = { enabled: true, delay: 30 }, off = { enabled: false, delay: 30 };

// cycle du clic : default → auto → none → default
eq('cycle 1', F.hideEyeNext('default'), 'auto');
eq('cycle 2', F.hideEyeNext('auto'), 'none');
eq('cycle 3', F.hideEyeNext('none'), 'default');
eq('cycle inconnu → default', F.hideEyeNext('???'), 'default');

// état visuel : ouvert = none, fermé = auto, grisé = default
for (const [m, g, open, dim] of [['none', on, true, false], ['none', off, true, false], ['auto', on, false, false], ['auto', off, false, false],
  ['default', on, false, true], ['default', off, true, true]]) {
  const v = F.hideEyeView(m, g);
  eq(`vue ${m} global=${g.enabled}`, { open: v.open, dim: v.dim }, { open, dim });
}
eq('noms', ['default', 'auto', 'none'].map((m) => F.hideEyeView(m, on).name), ['default', 'auto hide', 'no hide']);

// infobulle : valeur globale et prochain état
eq('infobulle auto', F.hideEyeView('auto', on).tip, 'Ce projet : auto hide — clic : no hide · réglage global : masqués après 30 s');
eq('infobulle none, global off', F.hideEyeView('none', off).tip, 'Ce projet : no hide — clic : default · réglage global : désactivé');
eq('infobulle default', F.hideEyeView('default', { enabled: true, delay: 45 }).tip, 'Ce projet : default — clic : auto hide · réglage global : masqués après 45 s');

// clic droit : menu des 3 états, état courant marqué
const m = F.hideEyeMenu('none', on);
eq('menu ordre', m.map((e) => e.mode), ['auto', 'default', 'none']);
eq('menu courant', m.map((e) => !!e.current), [false, false, true]);
eq('menu libellés', m.map((e) => e.label.split(' — ')[0]), ['auto hide', 'default', 'no hide']);
if (!/30 s/.test(m[0].label)) { console.log('KO menu : délai absent de « auto hide » : ' + m[0].label); ko++; } else console.log('OK menu délai');
if (!/désactivé/.test(F.hideEyeMenu('auto', off)[1].label)) { console.log('KO menu : valeur globale absente de « default »'); ko++; } else console.log('OK menu global');

// marque « own » (v2) : fond plein pour auto et none, jamais pour default, indépendante du réglage global
for (const g of [on, off, { enabled: true, delay: 0 }]) {
  eq('own auto', F.hideEyeView('auto', g).own, true);
  eq('own none', F.hideEyeView('none', g).own, true);
  eq('own default', F.hideEyeView('default', g).own, false);
}
eq('own XOR dim', ['default', 'auto', 'none'].map((m) => F.hideEyeView(m, on).own !== F.hideEyeView(m, on).dim), [true, true, true]);

// v2 : plus de puce « masquage N s » dans le titre de la carte Team (le délai n'est plus que dans l'infobulle et le menu)
const team = src.slice(src.indexOf("card('Team'"), src.indexOf("// Fichiers"));
if (!team.includes("card('Team'")) { console.log("KO : appel card('Team' introuvable"); ko++; }
else if (/masquage '|masquage \d|hidechip/.test(team)) { console.log("KO puce « masquage N s » encore passée à la carte Team"); ko++; } else console.log('OK pas de puce Team');
if (/'hidechip'/.test(src)) { console.log('KO classe hidechip encore utilisée dans app.js'); ko++; } else console.log('OK hidechip retirée');

// appel serveur
eq('apply auto', F.hideEyeApply('auto'), { hideIdle: 'auto' });
eq('apply default', F.hideEyeApply('default'), { hideIdle: 'default' });

console.log(ko ? `${ko} KO` : 'OK');
process.exit(ko ? 1 : 0);
