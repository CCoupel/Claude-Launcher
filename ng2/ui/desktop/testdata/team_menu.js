// Simulation (node) du menu « modèle · effort » de la carte TEAM (#250, maquette M1 A « Menu sur modèle · effort »).
// Usage : node team_menu.js app.js
// Fonctions pures de app.js (noms livrés par dev-frontend, comportement tiré de contracts/desktop-dispatch.md « v6.9.0 ») :
//   teamEffortsFor(table, model)  → efforts acceptés : table[model], sinon table['*'], sinon les 5 efforts
//   teamCellView(m, remote)       → { text, nx, clickable, locked, tip } ; text « modèle · effort » + ' ⟳' si m.running ;
//                                   remote → non cliquable, infobulle « réglage réservé à la fenêtre native » ;
//                                   m.editable !== true → non cliquable, infobulle « non réglable : … »
//   teamMenuView(m, table)        → { title, models[], efforts[], all, foot[], warn } ; chaque entrée { kind, value, current, tag, disabled, tip, label }
//        en tête de chaque rubrique : « Défaut (template) — <valeur> » (value 'default'), cochée ssi la source est 'template', JAMAIS grisée ;
//        effort non accepté par le modèle : grisé (disabled) + infobulle, jamais masqué ; valeur égale au template : tag « template = défaut » ;
//        ✓ (current) sur une valeur explicite ssi la source est 'companion' ; teamleader : pied de menu cite settings.json, avertissement /model /effort
//   teamSetArgs(path, m, what, value) → argument de teamAgentSet ; what 'all' → model/effort 'default'
//   teamSetFlash(res, m)          → { warn, text }
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(name) {
  const at = src.indexOf('function ' + name + '(');
  if (at < 0) { console.log('KO : function ' + name + '( absente de app.js'); process.exit(1); }
  let d = 0, end = -1;
  for (let k = src.indexOf('{', at); k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) { end = k + 1; break; } }
  return src.slice(at, end);
}
function grabConst(name) { // const NAME = ...; (sur une ligne, éventuellement plusieurs déclarations séparées par une virgule)
  const m = src.match(new RegExp('^const ' + name + '\\b[^\\n]*;', 'm'));
  if (!m) { console.log('KO : const ' + name + ' absente de app.js'); process.exit(1); }
  return m[0];
}
const names = ['teamEffortsFor', 'teamSrcLabel', 'teamCellView', 'teamMenuView', 'teamSetArgs', 'teamSetFlash'];
const ctx = { console, JSON, Array, Object };
vm.createContext(ctx);
vm.runInContext([grabConst('TEAM_MODELS'), grabConst('TEAM_REMOTE_TIP')].join('\n') + '\n' + names.map(grab).join('\n') +
  '\nglobalThis.F = { ' + names.join(', ') + ', TEAM_MODELS, TEAM_REMOTE_TIP };', ctx);
const F = ctx.F;
let ko = 0;
const eq = (name, got, want) => { const g = JSON.stringify(got), w = JSON.stringify(want); if (g !== w) { console.log(`KO ${name} : ${g}, attendu ${w}`); ko++; } else console.log('OK ' + name); };
const ok = (name, cond, info) => { if (!cond) { console.log('KO ' + name + (info ? ' : ' + info : '')); ko++; } else console.log('OK ' + name); };

const ALL5 = ['low', 'medium', 'high', 'xhigh', 'max'];
const TABLE = { opus: ALL5, sonnet: ALL5, haiku: ALL5, fable: ALL5, '*': ALL5 };
const TABLE_FULL = { opus: ALL5, sonnet: ALL5, haiku: ALL5, fable: ALL5, 'claude-opus-4-5': ['low', 'medium', 'high'], 'claude-haiku-4-5': [], '*': ALL5 };
const row = (o) => Object.assign({ name: 'dev-backend', agent: 'dev-backend', model: 'sonnet', effort: 'medium', modelSrc: 'template', effortSrc: 'template',
  tplModel: 'sonnet', tplEffort: 'medium', editable: true }, o || {});
const find = (list, value) => list.find((e) => e.value === value);

// ── teamEffortsFor : table unique, repli « * » ───────────────────────────
eq('efforts : modèle de la table', F.teamEffortsFor(TABLE_FULL, 'claude-opus-4-5'), ['low', 'medium', 'high']);
eq('efforts : aucun effort (haiku 4.5)', F.teamEffortsFor(TABLE_FULL, 'claude-haiku-4-5'), []);
eq('efforts : modèle inconnu → « * »', F.teamEffortsFor(TABLE_FULL, 'modele-inconnu'), ALL5);
eq('efforts : sans table → les 5 efforts', F.teamEffortsFor(undefined, 'opus'), ALL5);

// ── entrée « Défaut (template) » ──────────────────────────────────────────
{
  const v = F.teamMenuView(row(), TABLE);
  const dm = find(v.models, 'default'), de = find(v.efforts, 'default');
  eq('défaut modèle : première entrée', v.models[0].value, 'default');
  eq('défaut effort : première entrée', v.efforts[0].value, 'default');
  eq('défaut modèle : libellé avec la valeur du template', dm.label, 'Défaut (template) — sonnet');
  eq('défaut effort : libellé avec la valeur du template', de.label, 'Défaut (template) — medium');
  ok('défaut coché quand la clé est absente (source template)', dm.current === true && de.current === true);
  ok('aucune valeur explicite cochée en source template', v.models.filter((e) => e.value !== 'default').every((e) => !e.current) &&
    v.efforts.filter((e) => e.value !== 'default').every((e) => !e.current));
  eq('valeur égale au template : mention « template = défaut »', find(v.models, 'sonnet').tag, 'template = défaut');
  eq('effort égal au template : mention « template = défaut »', find(v.efforts, 'medium').tag, 'template = défaut');
  eq('autre valeur : pas de mention', find(v.models, 'opus').tag, '');
  eq('modèles proposés (après Défaut)', v.models.slice(1).map((e) => e.value), ['opus', 'sonnet', 'haiku', 'fable']);
  eq('efforts proposés (après Défaut)', v.efforts.slice(1).map((e) => e.value), ALL5);
}
{
  // valeur explicite posée dans le compagnon : ✓ sur elle, plus sur Défaut
  const v = F.teamMenuView(row({ model: 'opus', modelSrc: 'companion', effort: 'max', effortSrc: 'companion' }), TABLE);
  ok('✓ sur la valeur explicite (modèle)', find(v.models, 'opus').current === true && find(v.models, 'default').current === false);
  ok('✓ sur la valeur explicite (effort)', find(v.efforts, 'max').current === true && find(v.efforts, 'default').current === false);
  eq('défaut modèle garde la valeur du template dans son libellé', find(v.models, 'default').label, 'Défaut (template) — sonnet');
  ok('Défaut jamais grisé (modèle)', find(v.models, 'default').disabled === false);
  ok('Défaut jamais grisé (effort)', find(v.efforts, 'default').disabled === false);
}

// ── filtre des efforts selon le modèle (P2, P6) ───────────────────────────
{
  const v = F.teamMenuView(row({ model: 'claude-opus-4-5', modelSrc: 'companion' }), TABLE_FULL);
  eq('opus 4.5 : efforts grisés', v.efforts.filter((e) => e.disabled).map((e) => e.value), ['xhigh', 'max']);
  ok('opus 4.5 : effort grisé a une infobulle « non pris en charge »', /non pris en charge/.test(find(v.efforts, 'xhigh').tip), find(v.efforts, 'xhigh').tip);
  ok('opus 4.5 : efforts grisés restent affichés (jamais masqués)', v.efforts.length === 6);
  ok('opus 4.5 : « Défaut » jamais grisé', find(v.efforts, 'default').disabled === false);
  const w = F.teamMenuView(row({ model: 'claude-haiku-4-5', modelSrc: 'companion' }), TABLE_FULL);
  eq('haiku 4.5 : tous les efforts grisés', w.efforts.filter((e) => e.value !== 'default').every((e) => e.disabled), true);
  ok('haiku 4.5 : « Défaut » jamais grisé', find(w.efforts, 'default').disabled === false);
  const x = F.teamMenuView(row({ model: 'opus' }), TABLE_FULL);
  eq('alias : aucun effort grisé', x.efforts.filter((e) => e.disabled).length, 0);
  const y = F.teamMenuView(row({ model: 'inconnu-9' }), TABLE_FULL);
  eq('modèle inconnu : aucun effort grisé', y.efforts.filter((e) => e.disabled).length, 0);
}

// ── pied de menu, teamleader, « Tout par défaut » ─────────────────────────
{
  const v = F.teamMenuView(row({ model: 'opus', modelSrc: 'companion' }), TABLE);
  ok('pied : fichier compagnon cité', v.foot.join(' ').includes('dev-backend.md'), v.foot.join(' | '));
  ok('pied : pas de settings.json pour un teammate', !v.foot.join(' ').includes('settings.json'));
  ok('pas d’avertissement de session pour un teammate', !v.warn);
  ok('« Tout par défaut » cite les valeurs du template', /sonnet/.test(v.all.label) && /medium/.test(v.all.label), v.all.label);
  const t = F.teamMenuView(row({ name: 'leader', agent: 'teamleader', model: 'opus', modelSrc: 'companion' }), TABLE);
  ok('teamleader : pied cite settings.json', t.foot.join(' ').includes('settings.json'), t.foot.join(' | '));
  ok('teamleader : rappel /model et /effort', t.warn.includes('/model') && t.warn.includes('/effort'), t.warn);
}

// ── cellule « modèle · effort » : ⟳, accès distant, non réglable ──────────
{
  const c = F.teamCellView(row({ model: 'opus', effort: 'high' }), false);
  eq('cellule : texte', c.text, 'opus · high');
  ok('cellule réglable : cliquable', c.clickable === true && c.locked === false);
  ok('cellule : pas de ⟳ sans running', c.nx === false && !c.text.includes('⟳'));
  const r = F.teamCellView(row({ model: 'opus', effort: 'high', running: { model: 'sonnet' } }), false);
  ok('⟳ quand ce qui tourne diffère', r.nx === true && r.text === 'opus · high ⟳', r.text);
  ok('infobulle ⟳ : ce qui tourne et « au prochain spawn »', r.tip.includes('sonnet') && r.tip.includes('au prochain spawn'), r.tip);
  ok('effort du spawn inconnu : l’infobulle le dit', /inconnu/.test(r.tip), r.tip);
  const d = F.teamCellView(row({ running: { model: 'haiku', effort: 'low' } }), false);
  ok('running.effort connu : affiché dans l’infobulle', d.tip.includes('low'), d.tip);
  const rem = F.teamCellView(row(), true);
  ok('accès distant : non cliquable', rem.clickable === false && rem.locked === true);
  ok('accès distant : infobulle du contrat', rem.tip.includes('réglage réservé à la fenêtre native'), rem.tip);
  eq('accès distant : texte inchangé', rem.text, 'sonnet · medium');
  const ne = F.teamCellView(row({ editable: false, agent: 'cdp' }), false);
  ok('entrée non réglable : non cliquable, raison affichée', ne.clickable === false && /non réglable/.test(ne.tip), ne.tip);
  const hr = F.teamCellView(row({ editable: false, agent: '' }), false);
  ok('agent hors registre : non réglable', hr.clickable === false && /non réglable/.test(hr.tip), hr.tip);
  const nm = F.teamCellView(row({ model: '', effort: '' }), false);
  eq('valeurs absentes : tirets', nm.text, '— · —');
}

// ── arguments de teamAgentSet et messages ─────────────────────────────────
{
  const m = row();
  eq('args : modèle', F.teamSetArgs('/p', m, 'model', 'opus'), { path: '/p', agent: 'dev-backend', model: 'opus' });
  eq('args : effort', F.teamSetArgs('/p', m, 'effort', 'max'), { path: '/p', agent: 'dev-backend', effort: 'max' });
  eq('args : défaut modèle', F.teamSetArgs('/p', m, 'model', 'default'), { path: '/p', agent: 'dev-backend', model: 'default' });
  eq('args : tout par défaut', F.teamSetArgs('/p', m, 'all'), { path: '/p', agent: 'dev-backend', model: 'default', effort: 'default' });
  eq('args : nom d’agent du registre, pas le nom d’instance', F.teamSetArgs('/p', row({ name: 'planner', agent: 'implementation-planner' }), 'model', 'opus').agent, 'implementation-planner');
  const ok1 = F.teamSetFlash({ ok: true, model: 'opus', effort: 'max', file: 'dev-backend.md', note: 'appliqué au prochain spawn' }, m);
  ok('flash : succès', ok1.warn === false && ok1.text.includes('opus · max') && ok1.text.includes('dev-backend.md') && ok1.text.includes('prochain spawn'), ok1.text);
  const rm = F.teamSetFlash({ ok: true, model: 'sonnet', effort: 'medium', removed: true }, m);
  ok('flash : compagnon supprimé', rm.warn === false && /supprimé/.test(rm.text), rm.text);
  const adj = F.teamSetFlash({ ok: true, model: 'claude-opus-4-5', effort: 'medium', effortAdjusted: true }, m);
  ok('flash : effort ajusté dit', /ajusté/.test(adj.text), adj.text);
  const rf = F.teamSetFlash({ ok: false, remote: true, msg: 'réglage réservé à la fenêtre native' }, m);
  ok('flash : refus distant', rf.warn === true && rf.text.includes('réservé à la fenêtre native'), rf.text);
  const pa = F.teamSetFlash({ ok: false, partial: 'companion', msg: 'settings.json non écrit' }, m);
  ok('flash : partiel (compagnon écrit)', pa.warn === true && /compagnon écrit/.test(pa.text), pa.text);
  const to = F.teamSetFlash({ ok: false, timeout: true, msg: 'partage injoignable : dev-backend.md non écrit après 10 s' }, m);
  ok('flash : délai', to.warn === true && to.text.includes('partage injoignable'), to.text);
  ok('flash : sans réponse', F.teamSetFlash(null, m).warn === true);
}

console.log(ko ? ko + ' KO' : 'tout OK');
process.exit(ko ? 1 : 0);
