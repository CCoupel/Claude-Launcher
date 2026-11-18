// Simulation (node) des onglets de projets (#189 couleur hors sélection, #190 titre stable, #191 ordre par glisser-déposer).
// Usage : node tabs_bar.js app.js
// Contrat des fonctions pures exposées par app.js (maquette B : tab_bar__couleur_titre_ordre.html) :
//   tabView(t, on)                → { title, tip, color, gear, cls[] } ; title = t.name TOUJOURS (sélectionné ou non, occupé ou non) ;
//                                   tip = « agent au travail » seulement si t.busy (sélectionné ou non) ; color = t.color (ou défaut) même hors sélection ;
//                                   gear = true ssi t.busy (⚙ dans l'emplacement d'état, sélectionné ou non : .tab.on.busy, ⚙ blanc) ;
//                                   cls contient 'on' ssi sélectionné, 'busy' ssi t.busy (sélectionné OU non)
//   tabOrder(paths, saved)        → chemins dans l'ordre enregistré ; absents de saved à la fin (ordre d'origine) ; chemins disparus ignorés
//   tabMove(order, path, index)   → nouvel ordre (index borné) ; 'leaders' jamais déplaçable ; chemin inconnu ignoré
//   tabOrderLoad(store)           → tableau de chemins lu dans store.getItem('cl.tabOrder') ; [] si absent, invalide ou stockage qui lève
//   tabOrderSave(store, order)    → store.setItem('cl.tabOrder', JSON) ; ne lève jamais (stockage indisponible toléré)
//   tabCycleTarget(ids, cur, d)   → cible de Ctrl+Tab (d = ±1) dans l'ordre affiché, circulaire ; ids commence par 'leaders'
//   tabDragStep(state, ev)        → { state, action } : machine du glisser ; ev = {type:'down',path,x,onClose} | {type:'move',x} |
//                                   {type:'up'} | {type:'key',key} ; action = null | 'start' | 'drop' | 'cancel' | 'click'
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(name) {
  const at = src.indexOf('function ' + name + '(');
  if (at < 0) { console.log('KO : function ' + name + '( absente de app.js'); process.exit(1); }
  let d = 0, end = -1;
  for (let k = src.indexOf('{', at); k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) { end = k + 1; break; } }
  return src.slice(at, end);
}
const names = ['tabView', 'tabOrder', 'tabMove', 'tabOrderLoad', 'tabOrderSave', 'tabCycleTarget', 'tabDragStep'];
const ctx = { console, JSON };
vm.createContext(ctx);
vm.runInContext(names.map(grab).join('\n') + '\nglobalThis.F = { ' + names.join(', ') + ' };', ctx);
const F = ctx.F;
let ko = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { console.log(`KO ${name} : ${g}, attendu ${w}`); ko++; } else console.log('OK ' + name); };

// #190 titre stable ; #189 couleur hors sélection
const t = { name: 'alpha', path: '/p/alpha', color: '#C0392B', busy: false };
const tb = { ...t, busy: true };
eq('titre identique sélectionné', F.tabView(t, true).title, 'alpha');
eq('titre identique non sélectionné', F.tabView(t, false).title, 'alpha');
eq('titre sans « agent au travail » (occupé, non sélectionné)', F.tabView(tb, false).title, 'alpha');
eq('infobulle agent au travail', /agent au travail/.test(F.tabView(tb, false).tip), true);
eq('pas d\'infobulle si au repos', F.tabView(t, false).tip, '');
eq('couleur du projet hors sélection', F.tabView(t, false).color, '#C0392B');
eq('couleur du projet sélectionné', F.tabView(t, true).color, '#C0392B');
eq('classe on seulement si sélectionné', [F.tabView(t, true).cls.includes('on'), F.tabView(t, false).cls.includes('on')], [true, false]);
eq('classe busy sélectionné ou non (maquette B : .tab.on.busy)', [F.tabView(tb, false).cls.includes('busy'), F.tabView(tb, true).cls.includes('busy')], [true, true]);
eq('onglet sélectionné et occupé : on + busy', F.tabView(tb, true).cls.slice().sort(), ['busy', 'on']);
eq('⚙ présent dès que busy, sélectionné ou non', [F.tabView(tb, false).gear, F.tabView(tb, true).gear], [true, true]);
eq('pas de ⚙ au repos', [F.tabView(t, false).gear, F.tabView(t, true).gear], [false, false]);
eq('classe busy absente au repos', [F.tabView(t, false).cls.includes('busy'), F.tabView(t, true).cls.includes('busy')], [false, false]);
eq('titre inchangé : sélectionné et occupé', F.tabView(tb, true).title, 'alpha');
eq('infobulle aussi sélectionné et occupé', /agent au travail/.test(F.tabView(tb, true).tip), true);
eq('couleur par défaut si absente', typeof F.tabView({ name: 'x', path: '/x' }, false).color, 'string');

// #191 ordre
eq('ordre enregistré appliqué', F.tabOrder(['a', 'b', 'c'], ['c', 'a', 'b']), ['c', 'a', 'b']);
eq('nouveau projet à la fin', F.tabOrder(['a', 'b', 'c', 'n'], ['c', 'a', 'b']), ['c', 'a', 'b', 'n']);
eq('plusieurs nouveaux : ordre d\'origine', F.tabOrder(['n1', 'a', 'n2'], ['a']), ['a', 'n1', 'n2']);
eq('chemin disparu ignoré', F.tabOrder(['a', 'c'], ['b', 'c', 'a']), ['c', 'a']);
eq('rien d\'enregistré', F.tabOrder(['a', 'b'], []), ['a', 'b']);
eq('déplacer vers la fin', F.tabMove(['a', 'b', 'c'], 'a', 2), ['b', 'c', 'a']);
eq('déplacer vers le début', F.tabMove(['a', 'b', 'c'], 'c', 0), ['c', 'a', 'b']);
eq('index borné (trop grand)', F.tabMove(['a', 'b', 'c'], 'a', 99), ['b', 'c', 'a']);
eq('index borné (négatif)', F.tabMove(['a', 'b', 'c'], 'c', -5), ['c', 'a', 'b']);
eq('Teamleaders non déplaçable', F.tabMove(['a', 'b'], 'leaders', 1), ['a', 'b']);
eq('chemin inconnu ignoré', F.tabMove(['a', 'b'], 'zzz', 0), ['a', 'b']);
eq('déplacement sur place', F.tabMove(['a', 'b', 'c'], 'b', 1), ['a', 'b', 'c']);

// persistance
const mem = {}; const store = { getItem: (k) => (k in mem ? mem[k] : null), setItem: (k, v) => { mem[k] = v; } };
F.tabOrderSave(store, ['b', 'a']);
eq('clé cl.tabOrder', Object.keys(mem), ['cl.tabOrder']);
eq('relecture', F.tabOrderLoad(store), ['b', 'a']);
eq('stockage vide', F.tabOrderLoad({ getItem: () => null }), []);
eq('JSON invalide', F.tabOrderLoad({ getItem: () => '{pas du json' }), []);
eq('valeur non tableau', F.tabOrderLoad({ getItem: () => '{"a":1}' }), []);
const boom = { getItem() { throw new Error('SecurityError'); }, setItem() { throw new Error('QuotaExceeded'); } };
eq('lecture : stockage qui lève', F.tabOrderLoad(boom), []);
let threw = false; try { F.tabOrderSave(boom, ['a']); } catch (e) { threw = true; }
eq('écriture : stockage qui lève toléré', threw, false);

// Ctrl+Tab dans l'ordre affiché
eq('Ctrl+Tab suivant', F.tabCycleTarget(['leaders', 'c', 'a', 'b'], 'c', 1), 'a');
eq('Ctrl+Tab bouclé', F.tabCycleTarget(['leaders', 'c', 'a', 'b'], 'b', 1), 'leaders');
eq('Ctrl+Maj+Tab depuis Teamleaders', F.tabCycleTarget(['leaders', 'c', 'a', 'b'], 'leaders', -1), 'b');

// glisser
const run = (evs) => { let s = { phase: 'idle' }, acts = []; for (const e of evs) { const r = F.tabDragStep(s, e); s = r.state; acts.push(r.action); } return acts; };
eq('clic simple sans glisser', run([{ type: 'down', path: 'a', x: 100 }, { type: 'up' }]), [null, 'click']);
eq('sous 5 px : pas de glisser', run([{ type: 'down', path: 'a', x: 100 }, { type: 'move', x: 103 }, { type: 'up' }]), [null, null, 'click']);
eq('dès 5 px : glisser démarre', run([{ type: 'down', path: 'a', x: 100 }, { type: 'move', x: 105 }]), [null, 'start']);
eq('déposer', run([{ type: 'down', path: 'a', x: 100 }, { type: 'move', x: 130 }, { type: 'up' }]), [null, 'start', 'drop']);
eq('Échap annule', run([{ type: 'down', path: 'a', x: 100 }, { type: 'move', x: 130 }, { type: 'key', key: 'Escape' }, { type: 'up' }]), [null, 'start', 'cancel', null]);
eq('× ne lance jamais de glisser', run([{ type: 'down', path: 'a', x: 100, onClose: true }, { type: 'move', x: 160 }, { type: 'up' }]), [null, null, null]);
eq('Teamleaders ne se glisse pas', run([{ type: 'down', path: 'leaders', x: 10 }, { type: 'move', x: 90 }, { type: 'up' }]), [null, null, 'click']);
process.exit(ko ? 1 : 0);
