// ── Fenêtre « Réglages » (#118, LOT-3.1) ────────────────────────────────────
// Configuration structurée du launcher, selon la maquette
// docs/mockup/v6.1.0/ui/config_ui__interface_structuree.html (A : fenêtre
// modale large à rubriques). Rubriques Backends, Pools, Défauts (backends.yaml)
// lues par configModel et écrites par configApply (liste de changements : les
// commentaires du fichier sont conservés côté Go) ; les autres rubriques
// renvoient pour l'instant à l'éditeur YAML (« Éditer en YAML »).
// Chargé après app.js : utilise ses globales (el, $, call, flash, openModal,
// closeModal, configEditor, prefs, setFontSize, S).
const ST = {
  on: false, page: 'backends', project: '', model: null, orig: null, path: '', mtime: '', exists: true,
  loadProblems: [], draft: null, sel: { backend: 0, pool: 0 }, states: {}, load: {}, timer: null,
  result: null,        // dernier résultat d'enregistrement : {kind, problems, note, mtime}
  test: null,          // {name, running, steps}
};

const ST_TYPES = ['host', 'podman', 'docker', 'ssh', 'k8s'];
const ST_RAIL = [
  ['GLOBAL', [['backends', 'Backends'], ['pools', 'Pools'], ['defauts', 'Défauts'], ['launcher', 'Launcher'], ['conteneurs', 'Conteneurs']]],
  ['CE POSTE', [['poste', 'Ce poste']]],
  ['PROJET SÉLECTIONNÉ', [['projet', 'Projet']]],
];
const ST_KIND = { backends: 'backends.yaml', pools: 'backends.yaml', defauts: 'backends.yaml', launcher: 'config.conf', conteneurs: 'env.yaml', poste: 'backends.yaml', projet: 'project-env' };
const ST_FILE = { backends: 'backends.yaml', pools: 'backends.yaml', defauts: 'backends.yaml', launcher: 'config.conf', conteneurs: 'env.yaml (launcher)', poste: 'backends.yaml → hubs.<machine> ; desktop-ui.json', projet: '.claude-launcher/env.yaml du projet' };

const stClone = (o) => JSON.parse(JSON.stringify(o));
const stLines = (s) => String(s || '').split('\n').map((x) => x.trim()).filter(Boolean);

async function openSettings(project, page, focus) {
  ST.project = project || '';
  if (page) ST.page = page;
  ST.result = null; ST.test = null;
  ST.pj = null; ST.docs = {}; ST.draft = null; ST.loading = true; ST.states = {}; ST.load = {}; // fenêtre affichée tout de suite, données ensuite
  openModal((b) => {
    b.classList.add('wide');
    b.innerHTML = '';
    const head = el('div', 'cehead');
    const ttl = el('h2', '', 'Réglages'); const path = el('span', 'cepath'); path.id = 'stpath';
    const x = el('button', 'btn sm', '✕'); x.onclick = () => stClose();
    head.append(ttl, path, el('span', 'sp'), x);
    const body = el('div', 'stbody');
    const rail = el('div', 'strail'); rail.id = 'strail';
    const pg = el('div', 'stpg'); pg.id = 'stpg';
    body.append(rail, pg);
    const foot = el('div', 'stfoot'); foot.id = 'stfoot';
    b.append(head, body, foot);
  });
  ST.on = true;
  stRender();
  stLoad().then(() => { if (!ST.on) return; ST.loading = false; stRender(); });
  for (const id of Object.keys(DOCS)) docLoad(id); // chargements en parallèle, sans bloquer l'affichage
  stPoll(); // états des backends : appel séparé, ne retarde jamais l'affichage
  if (ST.project && ST.project !== 'new') { await pjLoad(); if (!ST.on) return; if (ST.pj && focus) ST.pj.focus = focus; stRenderPage(); stRenderFoot(); }
}

async function stLoad() {
  try {
    const r = await call('configModel', { kind: 'backends.yaml' });
    ST.model = r.model; ST.orig = stClone(r.model); ST.draft = stClone(r.model);
    ST.path = r.path; ST.mtime = r.mtime; ST.exists = r.exists; ST.loadProblems = r.problems || [];
    for (const b of ST.draft.backends || []) b._orig = b.name;
    for (const p of ST.draft.pools || []) p._orig = p.name;
    ST.draft.backends = ST.draft.backends || []; ST.draft.pools = ST.draft.pools || [];
    ST.draft.images = ST.draft.images || []; ST.draft.defaults = ST.draft.defaults || {};
  } catch (e) { ST.draft = null; ST.loadProblems = [{ severity: 'error', message: e.message }]; }
}

// états des backends et charge des pools : depuis le cache du diagnostic ; « pending » : relire plus tard
function stPoll() {
  clearTimeout(ST.timer);
  if (!ST.on) return;
  call('backendStates').then((r) => {
    if (!ST.on) return;
    ST.states = r.states || {}; ST.load = r.load || {};
    if (ST.page === 'backends' || ST.page === 'pools') stRenderPage(true);
    const pend = Object.values(ST.states).some((s) => s.pending);
    ST.timer = setTimeout(stPoll, pend ? 3000 : 30000);
  }).catch(() => { ST.timer = setTimeout(stPoll, 30000); });
}

// ── Changements = différence entre le brouillon et le fichier lu ────────────
function stBackendValue(b) {
  const v = {};
  for (const k of ['type', 'context', 'host', 'pool', 'engine', 'engine_host', 'namespace']) if (b[k]) v[k] = b[k];
  if (b.hosts && b.hosts.length) v.hosts = b.hosts;
  if (b.shared && b.shared.length) v.shared = b.shared;
  if (b.volumes && Object.keys(b.volumes).length) v.volumes = b.volumes;
  if (b.spread != null) v.spread = !!b.spread;
  if (b.pod && Object.keys(b.pod).length) v.pod = b.pod;
  return v;
}
// Renommages d'une section : ordonnés pour qu'aucun ne vise un nom encore pris
// (A→B pendant que B→C : B→C d'abord) ; un échange ou un cycle passe par un nom
// temporaire. Rend des changements « rename » sur le chemin [section, nom].
function stOrderRenames(section, pairs, existing) {
  const out = [], names = new Set(existing), todo = pairs.map((x) => ({ from: x.from, to: x.to }));
  let tmp = 0;
  while (todo.length) {
    const i = todo.findIndex((x) => !names.has(x.to));
    if (i >= 0) {
      const x = todo.splice(i, 1)[0];
      out.push({ op: 'rename', at: [section, x.from], value: x.to });
      names.delete(x.from); names.add(x.to);
    } else {
      // cycle : le premier passe par un nom temporaire, son renommage final reste à faire
      const x = todo[0]; let t;
      do { t = '__renommage_' + (++tmp); } while (names.has(t));
      out.push({ op: 'rename', at: [section, x.from], value: t });
      names.delete(x.from); names.add(t); x.from = t;
    }
  }
  return out;
}

function stBuildChanges() {
  const d = ST.draft, o = ST.orig;
  if (!d) return [];
  const del = [], ren = { backends: [], pools: [] }, set = [];
  const live = new Set(d.backends.map((b) => b._orig).filter(Boolean));
  for (const ob of o.backends) if (!ob.implicit && !live.has(ob.name)) del.push({ op: 'delete', at: ['backends', ob.name] });
  for (const b of d.backends) {
    if (b.implicit) continue;
    const ob = b._orig ? o.backends.find((x) => x.name === b._orig) : null;
    const val = stBackendValue(b);
    if (!ob) { set.push({ op: 'set', at: ['backends', b.name], value: val }); continue; }
    if (b.name !== ob.name) ren.backends.push({ from: ob.name, to: b.name });
    if (JSON.stringify(val) !== JSON.stringify(stBackendValue(ob))) set.push({ op: 'set', at: ['backends', b.name], value: val });
  }
  const livep = new Set(d.pools.map((p) => p._orig).filter(Boolean));
  for (const op of o.pools) if (!livep.has(op.name)) del.push({ op: 'delete', at: ['pools', op.name] });
  for (const p of d.pools) {
    const op = p._orig ? o.pools.find((x) => x.name === p._orig) : null;
    const val = { hosts: p.hosts || [] }; if (p.drain && p.drain.length) val.drain = p.drain;
    if (!op) { set.push({ op: 'set', at: ['pools', p.name], value: val }); continue; }
    if (p.name !== op.name) ren.pools.push({ from: op.name, to: p.name });
    const same = JSON.stringify(val) === JSON.stringify({ hosts: op.hosts || [], ...(op.drain && op.drain.length ? { drain: op.drain } : {}) });
    if (!same) set.push({ op: 'set', at: ['pools', p.name], value: val });
  }
  for (const k of ['backend', 'image']) {
    const a = d.defaults[k] || '', b = (o.defaults || {})[k] || '';
    if (a !== b) set.push(a ? { op: 'set', at: ['defaults', k], value: a } : { op: 'delete', at: ['defaults', k] });
  }
  if (JSON.stringify(d.images) !== JSON.stringify(o.images || [])) {
    set.push(d.images.length ? { op: 'set', at: ['images'], value: d.images } : { op: 'delete', at: ['images'] });
  }
  // suppressions, puis renommages ordonnés (noms libérés par les suppressions), puis créations et valeurs
  const delNames = (sec) => new Set(del.filter((c) => c.at[0] === sec).map((c) => c.at[1]));
  const exist = (sec) => (o[sec] || []).filter((x) => !x.implicit && !delNames(sec).has(x.name)).map((x) => x.name);
  return del.concat(stOrderRenames('backends', ren.backends, exist('backends')), stOrderRenames('pools', ren.pools, exist('pools')), set);
}

// problèmes locaux, avant tout envoi : nom vide ou en double, JSON du pod invalide
function stLocalProblems() {
  const ps = [], d = ST.draft;
  if (!d) return ps;
  const seen = new Set();
  for (const b of d.backends) {
    if (!b.name) ps.push({ severity: 'error', message: 'un backend n\'a pas de nom' });
    else if (seen.has(b.name)) ps.push({ severity: 'error', message: 'backend « ' + b.name + ' » en double' });
    seen.add(b.name);
    if (b._podErr) ps.push({ severity: 'error', message: 'backend « ' + b.name + ' » : options du pod : ' + b._podErr });
  }
  const sp = new Set();
  for (const p of d.pools) {
    if (!p.name) ps.push({ severity: 'error', message: 'un pool n\'a pas de nom' });
    else if (sp.has(p.name)) ps.push({ severity: 'error', message: 'pool « ' + p.name + ' » en double' });
    sp.add(p.name);
  }
  return ps;
}

// ── Cadre : rubriques, pied (problèmes, boutons) ────────────────────────────
function stRender() { stRenderRail(); stRenderPage(); stRenderFoot(); }

function stRenderRail() {
  const r = $('strail'); if (!r) return;
  r.textContent = '';
  for (const [g, items] of ST_RAIL) {
    r.appendChild(el('div', 'rg', g));
    for (const [k, t] of items) {
      const it = el('div', 'ri' + (k === ST.page ? ' on' : ''), t);
      it.onclick = () => { ST.page = k; ST.test = null; stRender(); };
      r.appendChild(it);
    }
  }
  $('stpath').textContent = ST_FILE[ST.page] || '';
}

function stRenderFoot() {
  const f = $('stfoot'); if (!f) return;
  f.textContent = '';
  if (ST.page === 'projet') return pjFoot(f);
  if (docOfPage()) return docFoot(f, docOfPage());
  const ch = stBuildChanges(), local = stLocalProblems();
  const res = ST.result;
  const probs = local.concat(res && res.problems ? res.problems : [], res ? [] : ST.loadProblems);
  const errs = probs.filter((p) => p.severity === 'error').length;
  const left = el('div', 'stmsg');
  if (res && res.conflict) {
    left.append(el('span', 't-orange', 'Le fichier a changé sur le disque depuis son ouverture. '));
    const rel = el('button', 'btn sm', 'Recharger (perdre mes modifications)'); rel.onclick = async () => { ST.result = null; await stLoad(); stRender(); };
    const ow = el('button', 'btn sm danger', 'Écraser avec ma version'); ow.onclick = () => { ST.mtime = res.mtime; ST.result = null; stSave(); };
    left.append(rel, ow);
  } else if (probs.length) {
    for (const p of probs) left.appendChild(el('div', 'ceprob ' + (p.severity === 'error' ? 't-red' : 't-yellow'),
      (p.severity === 'error' ? '✗ ' : '⚠ ') + (p.line ? 'ligne ' + p.line + ' : ' : '') + p.message));
  } else if (res && res.saved) {
    left.appendChild(el('span', 't-green', '✓ Enregistré (version précédente gardée en .bak). ' + (res.note || '')));
  } else if (ST.loading) {
    left.appendChild(el('span', 'dim', 'Chargement…'));
  } else {
    left.appendChild(el('span', 't-green', '✓ valide'));
  }
  const txt = el('button', 'btn', 'Éditer en YAML'); txt.onclick = () => stText(ST_KIND[ST.page]);
  const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = () => stClose();
  const save = el('button', 'btn pri', 'Enregistrer'); save.id = 'stsave';
  save.disabled = !ch.length || errs > 0 || !ST.draft || !stEditable();
  save.title = errs ? 'Corriger les erreurs avant d\'enregistrer' : !ch.length ? 'aucun changement' : '';
  save.onclick = () => stSave();
  f.append(left, el('span', 'sp'), txt, cancel, save);
}

const stEditable = () => ST.page === 'backends' || ST.page === 'pools' || ST.page === 'defauts';
const stDirty = () => stBuildChanges().length > 0 || pjDirty() || docDirty();

async function stSave() {
  if (ST.page === 'projet') return pjSave();
  if (docOfPage()) return docSave(docOfPage());
  const ch = stBuildChanges();
  if (!ch.length) return;
  let r;
  try { r = await call('configApply', { kind: 'backends.yaml', path: '', mtime: ST.mtime, changes: ch }); } catch (e) { return; }
  if (r.conflict) { ST.result = { conflict: true, mtime: r.mtime }; stRenderFoot(); return; }
  if (r.invalid) { ST.result = { problems: r.problems || [] }; stRenderFoot(); return; }
  const keep = { page: ST.page };
  await stLoad();
  ST.page = keep.page;
  ST.result = { saved: true, note: r.note || '', problems: (r.problems || []).filter((p) => p.severity !== 'error') };
  stRender();
  stPoll();
}

async function stText(kind) {
  if (stDirty() && !confirm('Abandonner les modifications non enregistrées de cette fenêtre ?')) return;
  const project = ST.project, from = ST.page;
  stClose(true);
  await configEditor(project, kind || '');
  CE.fromPage = from; // retour « ← Réglages » sur la rubrique d'où l'on vient
}

function stClose(force) {
  if (!force && stDirty() && !confirm('Fermer sans enregistrer ? Les modifications seront perdues.')) return;
  ST.on = false; clearTimeout(ST.timer);
  $('modalbox').classList.remove('wide');
  closeModal();
}

// ── Petits éléments de formulaire ──────────────────────────────────────────
function stInput(val, on, ph, cls) {
  const i = el('input', 'stin' + (cls ? ' ' + cls : '')); i.type = 'text'; i.value = val == null ? '' : val; i.spellcheck = false;
  if (ph) i.placeholder = ph;
  i.addEventListener('input', () => { on(i.value); stRenderFoot(); });
  return i;
}
function stSelect(val, opts, on) {
  const s = el('select', 'stin');
  for (const [v, t] of opts) { const o = el('option', '', t); o.value = v; s.appendChild(o); }
  s.value = val || ''; s.addEventListener('change', () => { on(s.value); stRenderFoot(); });
  return s;
}
function stCheck(on, text, fn) {
  const l = el('label', 'stchk'); const c = el('input'); c.type = 'checkbox'; c.checked = !!on;
  c.addEventListener('change', () => fn(c.checked));
  l.append(c, el('span', '', text)); return l;
}
function stArea(val, on, rows, ph) {
  const t = el('textarea', 'stin stta'); t.rows = rows || 3; t.value = val || ''; t.spellcheck = false;
  if (ph) t.placeholder = ph;
  t.addEventListener('input', () => { on(t.value); stRenderFoot(); });
  return t;
}
function stForm(rows) {
  const f = el('div', 'stform');
  for (const [label, node, hint] of rows) {
    f.appendChild(el('label', '', label));
    f.appendChild(node);
    if (hint) { f.appendChild(el('span')); f.appendChild(el('span', 'hint', hint)); }
  }
  return f;
}
function stBox(title, body, extra) {
  const b = el('div', 'stbox'); const h = el('div', 'bh'); h.append(el('span', '', title), el('span', 'sp'));
  if (extra) h.append(extra);
  b.append(h, body); body.classList.add('bb'); return b;
}
const stBtn = (t, fn, cls) => { const b = el('button', 'btn sm' + (cls ? ' ' + cls : ''), t); b.onclick = fn; return b; };

function stRenderPage(soft) {
  const p = $('stpg'); if (!p) return;
  // rafraîchissement doux (états reçus) : ne pas écraser une saisie en cours
  if (soft && p.contains(document.activeElement) && /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName)) return;
  const top = p.scrollTop;
  p.textContent = '';
  if (ST.loading && stEditable()) { p.appendChild(el('p', 'dim', 'Chargement…')); return; }
  if (!ST.draft && stEditable()) { p.appendChild(el('div', 't-red', 'Lecture impossible : ' + ((ST.loadProblems[0] || {}).message || 'erreur') + ' — utiliser « Éditer en YAML ».')); return; }
  const fn = { backends: stPageBackends, pools: stPagePools, defauts: stPageDefaults, launcher: stPageConf, conteneurs: stPageEnv, poste: stPageHub, projet: stPageProjet }[ST.page];
  fn(p);
  p.scrollTop = top;
}

// ── Backends ───────────────────────────────────────────────────────────────
function stStateCell(name) {
  const s = ST.states[name];
  if (!s) return el('span', 'dim', '…');
  if (s.pending) return el('span', 'dim', '… test en cours');
  const c = el('span', s.ok ? 't-green' : 't-yellow', (s.ok ? '● ' : '⚠ ') + (s.text || ''));
  c.title = s.text || ''; // texte complet en infobulle (la cellule le coupe par « … »)
  return c;
}
function stAddr(b) { return b.host || (b.hosts && b.hosts.length ? b.hosts.join(', ') : '') || (b.type === 'k8s' ? [b.context, b.namespace].filter(Boolean).join(' / ') : (b.type === 'host' ? '' : '(local)')); }

function stPageBackends(p) {
  const d = ST.draft;
  p.append(el('h4', '', 'Backends'), el('p', 'ex', 'Où s\'exécutent les projets : sur l\'hôte, dans un conteneur local ou distant, ou dans un pod Kubernetes.'));
  const split = el('div', 'stsplit'); p.appendChild(split);
  const left = el('div', 'stleft'); split.appendChild(left);
  const t = el('table', 'sttable'); const hr = el('tr');
  for (const h of ['Nom', 'Type', 'Adresse', 'Pool', 'État']) hr.appendChild(el('th', '', h));
  t.appendChild(hr);
  if (ST.sel.backend >= d.backends.length) ST.sel.backend = Math.max(0, d.backends.length - 1);
  d.backends.forEach((b, i) => {
    const r = el('tr', i === ST.sel.backend ? 'on' : '');
    const n = el('td', 'm', b.name || '(sans nom)');
    if (d.defaults.backend && d.defaults.backend === b.name) n.appendChild(el('span', 'pill', 'défaut'));
    r.append(n, el('td', '', b.type), (() => { const c = el('td', 'm sta', stAddr(b)); c.title = stAddr(b); return c; })(), el('td', 'm', b.pool || ''), el('td'));
    r.lastChild.className = 'stst'; r.lastChild.appendChild(b._orig ? stStateCell(b._orig) : el('span', 'dim', 'nouveau'));
    r.onclick = () => { ST.sel.backend = i; ST.test = null; stRenderPage(); };
    t.appendChild(r);
  });
  left.appendChild(t);
  const bar = el('div', 'stbar');
  bar.append(stBtn('+ Ajouter', () => { d.backends.push({ name: stUniqueName('nouveau', d.backends), type: 'docker' }); ST.sel.backend = d.backends.length - 1; ST.test = null; stRenderPage(); stRenderFoot(); }),
    stBtn('Dupliquer', () => {
      const b = d.backends[ST.sel.backend]; if (!b || b.implicit) return;
      const c = stClone(b); delete c._orig; delete c.implicit; c.name = stUniqueName(b.name + '-copie', d.backends);
      d.backends.push(c); ST.sel.backend = d.backends.length - 1; ST.test = null; stRenderPage(); stRenderFoot();
    }),
    stBtn('Supprimer', () => {
      const b = d.backends[ST.sel.backend]; if (!b || b.implicit) return;
      const isDef = !!b.name && d.defaults.backend === b.name;
      if (!confirm('Supprimer le backend « ' + b.name + ' » ?' + (isDef ? '\nC\'est le backend par défaut : il sera retiré des défauts dans le même enregistrement.' : ''))) return;
      if (isDef) d.defaults.backend = '';
      d.backends.splice(ST.sel.backend, 1); ST.test = null; stRenderPage(); stRenderFoot();
    }, 'dg'));
  left.appendChild(bar);
  left.appendChild(el('p', 'note', 'podman / docker : adresse (tcp:// ou ssh://) ou groupe de serveurs ou pool, contexte, stockage partagé. ssh : destination(s) ou pool, moteur, adresse du démon distant. k8s : contexte, namespace, volumes (racine → PVC), répartition, options du pod. hôte : aucun champ.'));
  const b = d.backends[ST.sel.backend];
  if (b) split.appendChild(stBackendForm(b));
}

function stUniqueName(base, list) {
  let n = base, i = 2;
  while (list.some((x) => x.name === n)) n = base + '-' + i++;
  return n;
}

function stBackendForm(b) {
  const d = ST.draft;
  const body = el('div');
  if (b.implicit) { body.appendChild(el('p', 'note', 'Backend « host » implicite (Windows) : exécution directe sur le poste, rien à régler.')); return stBox(b.name, body); }
  const rename = (v) => {
    const old = b.name;
    b.name = v;
    if (d.defaults.backend === old) d.defaults.backend = v;
  };
  const rows = [['Nom', stInput(b.name, rename)]];
  const typeSel = stSelect(b.type, ST_TYPES.map((x) => [x, x]), (v) => { b.type = v; ST.test = null; stRenderPage(); });
  rows.push(['Type', typeSel]);
  const t = b.type;
  if (t === 'podman' || t === 'docker' || t === 'ssh') {
    const mode = b.pool ? 'pool' : (b.hosts && b.hosts.length) ? 'groupe' : 'adresse';
    const m = el('span', 'stmode');
    for (const [v, txt] of [['adresse', 'une adresse'], ['groupe', 'groupe'], ['pool', 'pool']]) {
      const l = el('label', 'stchk'); const r = el('input'); r.type = 'radio'; r.name = 'stmode'; r.checked = mode === v;
      r.onchange = () => {
        if (v === 'adresse') { b.hosts = []; b.pool = ''; } else if (v === 'groupe') { b.host = ''; b.pool = ''; if (!b.hosts || !b.hosts.length) b.hosts = ['']; } else { b.host = ''; b.hosts = []; if (!b.pool) b.pool = (d.pools[0] || {}).name || ''; }
        stRenderPage(); stRenderFoot();
      };
      l.append(r, el('span', '', txt)); m.appendChild(l);
    }
    rows.push(['Serveurs', m]);
    if (mode === 'adresse') rows.push([t === 'ssh' ? 'Destination' : 'Adresse', stInput(b.host, (v) => { b.host = v; }, t === 'ssh' ? 'utilisateur@hôte' : 'tcp://hôte:port ou ssh://utilisateur@hôte'),
      t === 'ssh' ? 'utilisateur@hôte' : 'tcp://hôte:port ou ssh://utilisateur@hôte ; vide : démon local']);
    else if (mode === 'groupe') rows.push(['Serveurs du groupe', stArea((b.hosts || []).join('\n'), (v) => { b.hosts = stLines(v); }, 3), 'un par ligne ; le moins chargé reçoit le conteneur']);
    else rows.push(['Pool', stSelect(b.pool, d.pools.map((x) => [x.name, x.name]), (v) => { b.pool = v; }), 'machines du pool : voir la rubrique Pools']);
    if (t === 'ssh') {
      rows.push(['Moteur', stSelect(b.engine || 'podman', [['podman', 'podman'], ['docker', 'docker']], (v) => { b.engine = v; })]);
      rows.push(['Adresse du démon', stInput(b.engine_host, (v) => { b.engine_host = v; }), 'adresse du démon vu depuis le serveur ; facultatif']);
    } else rows.push(['Contexte ' + t, stInput(b.context, (v) => { b.context = v; }, '(aucun)')]);
    rows.push(['Stockage partagé', stArea((b.shared || []).join('\n'), (v) => { b.shared = stLines(v); }, 2), 'racines vues à l\'identique par le launcher et le serveur ; une par ligne']);
  } else if (t === 'k8s') {
    rows.push(['Contexte', stInput(b.context, (v) => { b.context = v; })]);
    rows.push(['Namespace', stInput(b.namespace, (v) => { b.namespace = v; })]);
    rows.push(['Volumes', stArea(Object.entries(b.volumes || {}).map(([k, v]) => k + '=' + v).join('\n'), (v) => {
      const o = {}; for (const l of stLines(v)) { const i = l.indexOf('='); if (i > 0) o[l.slice(0, i).trim()] = l.slice(i + 1).trim(); }
      b.volumes = o;
    }, 3), 'racine=PVC, une par ligne']);
    rows.push(['Répartition', stCheck(b.spread, 'répartir les pods sur les nœuds', (v) => { b.spread = v; stRenderFoot(); })]);
    rows.push(['Options du pod', stArea(b.pod && Object.keys(b.pod).length ? JSON.stringify(b.pod, null, 2) : '', (v) => {
      b._podErr = '';
      if (!v.trim()) { b.pod = {}; return; }
      try { const o = JSON.parse(v); if (typeof o !== 'object' || Array.isArray(o)) throw new Error('un objet est attendu'); b.pod = o; } catch (e) { b._podErr = e.message; }
    }, 5, '{ "nodeSelector": { … } }'), 'objet JSON (nodeSelector, tolerations, resources…)']);
  }
  rows.push(['', stCheck(d.defaults.backend === b.name && !!b.name, 'backend par défaut', (v) => { if (v) d.defaults.backend = b.name; else if (d.defaults.backend === b.name) d.defaults.backend = ''; stRenderFoot(); })]);
  body.appendChild(stForm(rows));
  // test par étapes (jusqu'à 45 s : état « en cours »)
  const test = el('button', 'btn sm pri', ST.test && ST.test.running && ST.test.name === b.name ? 'Test en cours…' : 'Tester');
  test.disabled = !!(ST.test && ST.test.running);
  test.onclick = () => stTest(b);
  const out = el('div', 'sttests'); out.id = 'sttests';
  stFillTests(out, b);
  body.appendChild(out);
  return stBox(b.name || '(sans nom)', body, test);
}

function stFillTests(out, b) {
  out.textContent = '';
  const t = ST.test;
  if (!t || t.name !== b.name) { out.classList.add('hidden'); return; }
  out.classList.remove('hidden');
  if (t.running) { out.appendChild(el('div', 'dim', 'test en cours… (jusqu\'à 45 s)')); return; }
  for (const s of t.steps || []) {
    const r = el('div'); r.append(el('span', s.ok ? 't-green' : 't-red', s.ok ? '✓ ' : '✗ '), el('span', '', s.label + (s.detail ? ' : ' + s.detail : '')));
    out.appendChild(r);
    if (!s.ok && s.fix) out.appendChild(el('div', 'dim', '   → ' + s.fix));
  }
  if (t.err) out.appendChild(el('div', 't-red', t.err));
}

async function stTest(b) {
  const f = stBackendValue(b); f.name = b.name;
  ST.test = { name: b.name, running: true, steps: [] };
  stRenderPage();
  try {
    const r = await call('backendTest', { backend: f });
    ST.test = { name: b.name, running: false, steps: r.steps || [] };
  } catch (e) { ST.test = { name: b.name, running: false, steps: [], err: e.message }; }
  if (ST.on) stRenderPage();
}

// ── Pools ──────────────────────────────────────────────────────────────────
function stPagePools(p) {
  const d = ST.draft;
  p.append(el('h4', '', 'Pools de machines'), el('p', 'ex', 'Un pool est une liste de machines que plusieurs backends se partagent ; un nouveau conteneur va sur la machine la moins chargée, hors drain.'));
  const split = el('div', 'stsplit'); p.appendChild(split);
  const left = el('div'); split.appendChild(left);
  const t = el('table', 'sttable'); const hr = el('tr');
  for (const h of ['Pool', 'Machines', 'Utilisé par']) hr.appendChild(el('th', '', h));
  t.appendChild(hr);
  if (ST.sel.pool >= d.pools.length) ST.sel.pool = Math.max(0, d.pools.length - 1);
  d.pools.forEach((pl, i) => {
    const used = d.backends.filter((b) => b.pool && b.pool === pl.name).map((b) => b.name);
    const r = el('tr', i === ST.sel.pool ? 'on' : '');
    r.append(el('td', 'm', pl.name || '(sans nom)'), el('td', '', (pl.hosts || []).length + ((pl.drain || []).length ? ' · ' + pl.drain.length + ' en drain' : '')),
      el('td', 'm' + (used.length ? '' : ' dim'), used.length ? used.join(', ') : '(aucun backend)'));
    r.onclick = () => { ST.sel.pool = i; stRenderPage(); };
    t.appendChild(r);
  });
  left.appendChild(t);
  left.appendChild(el('div', 'stbar')).append(
    stBtn('+ Ajouter', () => { d.pools.push({ name: stUniqueName('pool', d.pools), hosts: [], drain: [] }); ST.sel.pool = d.pools.length - 1; stRenderPage(); stRenderFoot(); }),
    stBtn('Supprimer', () => {
      const pl = d.pools[ST.sel.pool]; if (!pl) return;
      if (!confirm('Supprimer le pool « ' + pl.name + ' » ?')) return;
      d.pools.splice(ST.sel.pool, 1); stRenderPage(); stRenderFoot();
    }, 'dg'));
  const pl = d.pools[ST.sel.pool];
  if (!pl) return;
  const body = el('div');
  body.appendChild(stForm([['Nom', stInput(pl.name, (v) => {
    const old = pl.name; pl.name = v;
    for (const b of d.backends) if (b.pool === old) b.pool = v; // les backends qui citent ce pool suivent
  })]]));
  const mt = el('table', 'sttable'); const mh = el('tr');
  for (const h of ['Machine', 'Drain', 'Charge commune', '']) mh.appendChild(el('th', '', h));
  mt.appendChild(mh);
  const load = ST.load[pl._orig || pl.name] || {};
  (pl.hosts || []).forEach((m, i) => {
    const r = el('tr');
    const dr = (pl.drain || []).includes(m);
    r.append(el('td', 'm', m), el('td'), el('td', 'm' + (load[m] ? '' : ' dim'), load[m] ? load[m] + ' conteneur' + (load[m] > 1 ? 's' : '') : '—'), el('td'));
    r.children[1].appendChild(stCheck(dr, '', (v) => { pl.drain = (pl.drain || []).filter((x) => x !== m); if (v) pl.drain.push(m); stRenderPage(); stRenderFoot(); }));
    r.children[3].appendChild(stBtn('✕', () => { pl.hosts.splice(i, 1); pl.drain = (pl.drain || []).filter((x) => x !== m); stRenderPage(); stRenderFoot(); }, 'dg'));
    mt.appendChild(r);
  });
  body.appendChild(mt);
  const add = el('div', 'stbar'); const ni = stInput('', () => {}, 'nouvelle machine', 'w');
  const doAdd = () => { const v = ni.value.trim(); if (!v || (pl.hosts || []).includes(v)) return; pl.hosts = (pl.hosts || []).concat(v); stRenderPage(); stRenderFoot(); };
  ni.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); doAdd(); } });
  add.append(ni, stBtn('+ Ajouter', doAdd));
  body.append(add, el('p', 'note', 'Drain : la machine ne reçoit plus de nouveau conteneur ; ceux en cours continuent. La charge compte les conteneurs de tous les backends du pool.'));
  split.appendChild(stBox(pl.name || '(sans nom)', body));
}

// ── Défauts ────────────────────────────────────────────────────────────────
function stPageDefaults(p) {
  const d = ST.draft;
  p.append(el('h4', '', 'Défauts'), el('p', 'ex', 'Valeurs proposées à la création d\'un projet et à la première ouverture d\'un projet non configuré.'));
  const g = el('div', 'stsplit'); p.appendChild(g);
  const a = el('div');
  const bopts = [['', '(aucun)']].concat(d.backends.filter((b) => b.name).map((b) => [b.name, b.name]));
  const iopts = [['', '(aucune)']].concat(d.images.map((i) => [i, i]));
  if (d.defaults.image && !d.images.includes(d.defaults.image)) iopts.push([d.defaults.image, d.defaults.image]);
  a.appendChild(stForm([['Backend par défaut', stSelect(d.defaults.backend, bopts, (v) => { d.defaults.backend = v; })],
    ['Image par défaut', stSelect(d.defaults.image, iopts, (v) => { d.defaults.image = v; })]]));
  a.appendChild(el('p', 'stsub', 'Images proposées'));
  const t = el('table', 'sttable');
  d.images.forEach((im, i) => {
    const r = el('tr'); r.append(el('td', 'm', im), el('td'));
    r.lastChild.appendChild(stBtn('✕', () => { d.images.splice(i, 1); if (d.defaults.image === im) d.defaults.image = ''; stRenderPage(); stRenderFoot(); }, 'dg'));
    t.appendChild(r);
  });
  a.appendChild(t);
  const add = el('div', 'stbar'); const ni = stInput('', () => {}, 'image:tag', 'w');
  const doAdd = () => { const v = ni.value.trim(); if (!v || d.images.includes(v)) return; d.images.push(v); stRenderPage(); stRenderFoot(); };
  ni.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); doAdd(); } });
  add.append(ni, stBtn('+ Ajouter', doAdd)); a.appendChild(add);
  g.appendChild(stBox('Exécution', a));
  // navigateur : affiché, modifiable en YAML (rubrique « Ce poste » au sous-lot suivant)
  const n = el('div');
  const bs = (d.defaults.browser_server) || null, bp = (d.defaults.browser_paths) || [];
  n.appendChild(el('p', 'note', 'Défaut de tous les postes, utilisé quand « Ce poste » ne définit rien. Modifiable pour l\'instant par « Éditer en YAML ».'));
  n.appendChild(stForm([['Serveur web', el('span', 'm', bs && bs.listen ? bs.listen : '(défaut)')], ['URL publiée', el('span', 'm', bs && bs.url ? bs.url : '(déduite)')]]));
  n.appendChild(el('p', 'stsub', 'Chemins pour le navigateur'));
  if (bp.length) {
    const t2 = el('table', 'sttable'); const h2 = el('tr'); for (const h of ['Vu par claude', 'Vu par le navigateur', 'Forme']) h2.appendChild(el('th', '', h)); t2.appendChild(h2);
    for (const x of bp) { const r = el('tr'); r.append(el('td', 'm', x.container || ''), el('td', 'm', x.host || ''), el('td', '', x.os || '')); t2.appendChild(r); }
    n.appendChild(t2);
  } else n.appendChild(el('div', 'dim', '(aucun)'));
  g.appendChild(stBox('Navigateur', n));
}

// ── Rubriques des sous-lots suivants : renvoi à l'éditeur de texte ──────────
// ── Launcher, Conteneurs, Ce poste (#149, LOT-3.3) ──────────────────────────
// Trois documents lus par configModel (config.conf, env.yaml du launcher, hub =
// hubs.<machine> de backends.yaml) et écrits par configApply, chacun avec son
// brouillon, sa date, son résultat : même comportement que backends.yaml.
const DOCS = { conf: { kind: 'config.conf', page: 'launcher' }, env: { kind: 'env.yaml', page: 'conteneurs' }, hub: { kind: 'hub', page: 'poste' } };
const docOfPage = () => Object.keys(DOCS).find((k) => DOCS[k].page === ST.page) || '';

function docDraftOf(id, m) {
  const d = {};
  if (id === 'conf') {
    d.v = {}; d.reset = {}; d.token = ''; d.tokenDel = false;
    for (const x of m.settings || []) d.v[x.key] = confDisplay(x, x.value);
  } else if (id === 'env') {
    d.txt = { packages: (m.packages || []).join('\n'), setup: (m.setup || []).join('\n'), on_start: (m.on_start || []).join('\n'), pbm: pjPbmText(m.packages_by_manager) };
  } else {
    const s = m.settings || {};
    d.txt = {
      paths: (s.paths || []).map((x) => x.local + ' => ' + x.remote).join('\n'),
      browser_paths: (s.browser_paths || []).map((x) => x.container + ' => ' + x.host + (x.os ? ' | ' + x.os : '')).join('\n'),
      allow: ((s.browser_server || {}).allow || []).join('\n'),
    };
    const bs = s.browser_server || {}, ru = s.remote_user || {};
    d.f = { listen: bs.listen || '', url: bs.url || '', uid: ru.uid == null ? '' : String(ru.uid), gid: ru.gid == null ? '' : String(ru.gid), name: ru.name || '', home: ru.home || '', hub_cache: s.hub_cache || '', browser_device: s.browser_device || '' };
  }
  return d;
}
function confDisplay(x, v) {
  switch (x.type) {
    case 'bool': return !!v && v !== '0' && v !== 0;
    case 'int': return v == null ? '' : String(v);
    case 'list': return (v || []).join('\n');
    case 'intlist': return (v || []).join(' ');
    default: return v == null ? '' : String(v);
  }
}
function confValue(x, v) {
  switch (x.type) {
    case 'bool': return !!v;
    case 'int': return Number(v);
    case 'list': return stLines(v);
    case 'intlist': return String(v).split(/[\s,]+/).filter(Boolean).map(Number);
    default: return String(v);
  }
}

async function docLoad(id, keep) {
  const dd = DOCS[id];
  if (!keep || !ST.docs[id]) ST.docs[id] = { loading: true };
  try {
    const r = await call('configModel', { kind: dd.kind });
    if (!ST.on) return;
    const old = ST.docs[id];
    ST.docs[id] = { loading: false, model: r.model, path: r.path, mtime: r.mtime, exists: r.exists, problems: r.problems || [], d: docDraftOf(id, r.model), o: docDraftOf(id, r.model), res: keep && old ? old.res : null };
  } catch (e) { ST.docs[id] = { loading: false, err: e.message }; }
  if (ST.on && ST.page === dd.page) { stRenderPage(true); stRenderFoot(); }
}

function docChanges(id) {
  const D = ST.docs[id]; if (!D || !D.d) return [];
  const d = D.d, o = D.o, ch = [], put = (at, nv, ov, empty) => {
    if (JSON.stringify(nv) === JSON.stringify(ov)) return;
    ch.push(empty ? { op: 'delete', at } : { op: 'set', at, value: nv });
  };
  if (id === 'conf') {
    for (const x of D.model.settings || []) {
      if (!x.editable) continue;
      if (x.type === 'secret') {
        if (d.tokenDel && x.set) ch.push({ op: 'delete', at: [x.key] });
        else if (d.token) ch.push({ op: 'set', at: [x.key], value: d.token });
        continue;
      }
      if (d.reset[x.key] && x.set) { ch.push({ op: 'delete', at: [x.key] }); continue; }
      if (d.v[x.key] !== o.v[x.key]) {
        const val = confValue(x, d.v[x.key]);
        if (!(x.type === 'int' && isNaN(val))) ch.push({ op: 'set', at: [x.key], value: val });
      }
    }
  } else if (id === 'env') {
    for (const k of ['packages', 'setup', 'on_start']) put([k], stLines(d.txt[k]), stLines(o.txt[k]), !stLines(d.txt[k]).length);
    const nm = pjParsePbm(d.txt.pbm), om = pjParsePbm(o.txt.pbm);
    if (pjSorted(nm) !== pjSorted(om)) ch.push(Object.keys(nm).length ? { op: 'set', at: ['packages_by_manager'], value: nm } : { op: 'delete', at: ['packages_by_manager'] });
  } else {
    const pa = (t) => stLines(t).map((l) => { const [a, b] = l.split('=>'); return { local: (a || '').trim(), remote: (b || '').trim() }; });
    const bp = (t) => stLines(t).map((l) => { const [a, rest] = l.split('=>'); const [h, os] = (rest || '').split('|'); const r = { container: (a || '').trim(), host: (h || '').trim() }; if (os && os.trim()) r.os = os.trim(); return r; });
    put(['paths'], pa(d.txt.paths), pa(o.txt.paths), !pa(d.txt.paths).length);
    put(['browser_paths'], bp(d.txt.browser_paths), bp(o.txt.browser_paths), !bp(d.txt.browser_paths).length);
    const bsv = (D2) => { const v = {}; if (D2.f.listen.trim()) v.listen = D2.f.listen.trim(); if (D2.f.url.trim()) v.url = D2.f.url.trim(); const a = stLines(D2.txt.allow); if (a.length) v.allow = a; return v; };
    put(['browser_server'], bsv(d), bsv(o), !Object.keys(bsv(d)).length);
    const ruv = (f) => { const v = {}; if (f.uid !== '') v.uid = Number(f.uid); if (f.gid !== '') v.gid = Number(f.gid); if (f.name.trim()) v.name = f.name.trim(); if (f.home.trim()) v.home = f.home.trim(); return v; };
    put(['remote_user'], ruv(d.f), ruv(o.f), !Object.keys(ruv(d.f)).length);
    for (const k of ['hub_cache', 'browser_device']) put([k], d.f[k].trim(), o.f[k].trim(), !d.f[k].trim());
  }
  return ch;
}
function docLocalProblems(id) {
  const D = ST.docs[id], ps = [];
  if (!D || !D.d) return ps;
  if (id === 'conf') {
    for (const x of D.model.settings || []) {
      if (!x.editable || x.type !== 'int' || D.d.reset[x.key]) continue;
      const v = D.d.v[x.key], n = Number(v);
      if (v === '' || isNaN(n) || !Number.isInteger(n) || (x.min != null && n < x.min) || (x.max != null && n > x.max))
        ps.push({ severity: 'error', message: x.label + ' : entier' + (x.min != null ? ' ≥ ' + x.min : '') + (x.max != null ? ' ≤ ' + x.max : '') + ' attendu' });
    }
  } else if (id === 'hub') {
    for (const k of ['uid', 'gid']) if (D.d.f[k] !== '' && !/^\d+$/.test(D.d.f[k])) ps.push({ severity: 'error', message: 'utilisateur distant : ' + k + ' numérique attendu' });
  }
  return ps;
}
const docDirty = () => Object.keys(DOCS).some((id) => docChanges(id).length > 0);

async function docSave(id) {
  const D = ST.docs[id]; const ch = docChanges(id); if (!D || !ch.length) return;
  let r;
  try { r = await call('configApply', { kind: DOCS[id].kind, path: '', mtime: D.mtime, changes: ch }); } catch (e) { return; }
  if (r.conflict) { D.res = { conflict: true, mtime: r.mtime }; stRenderFoot(); return; }
  if (r.invalid) { D.res = { problems: r.problems || [] }; stRenderFoot(); return; }
  const warn = (r.problems || []).filter((p) => p.severity !== 'error');
  await docLoad(id);
  if (ST.docs[id]) ST.docs[id].res = { saved: true, note: r.note || '', problems: warn };
  stRender();
}

function docFoot(f, id) {
  const D = ST.docs[id] || { loading: true };
  const left = el('div', 'stmsg');
  const res = D.res;
  const probs = docLocalProblems(id).concat(res && res.problems ? res.problems : [], res ? [] : D.problems || []);
  const errs = probs.filter((p) => p.severity === 'error').length;
  if (res && res.conflict) {
    left.append(el('span', 't-orange', 'Le fichier a changé sur le disque depuis son ouverture. '));
    const rel = el('button', 'btn sm', 'Recharger (perdre mes modifications)'); rel.onclick = async () => { await docLoad(id); stRender(); };
    const ow = el('button', 'btn sm danger', 'Écraser avec ma version'); ow.onclick = () => { D.mtime = res.mtime; D.res = null; docSave(id); };
    left.append(rel, ow);
  } else if (probs.length) {
    for (const p of probs) left.appendChild(el('div', 'ceprob ' + (p.severity === 'error' ? 't-red' : 't-yellow'), (p.severity === 'error' ? '✗ ' : '⚠ ') + (p.line ? 'ligne ' + p.line + ' : ' : '') + p.message));
  } else if (res && res.saved) {
    left.appendChild(el('span', 't-green', '✓ Enregistré (version précédente gardée en .bak). ' + (res.note || '')));
  } else if (D.loading) left.appendChild(el('span', 'dim', 'Chargement…'));
  else left.appendChild(el('span', 't-green', '✓ valide'));
  const txt = el('button', 'btn', 'Éditer en YAML'); txt.onclick = () => stText(ST_KIND[ST.page]);
  const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = () => stClose();
  const save = el('button', 'btn pri', 'Enregistrer'); save.id = 'stsave';
  const n = docChanges(id).length;
  save.disabled = !n || errs > 0 || !D.d;
  save.title = errs ? 'Corriger les erreurs avant d\'enregistrer' : !n ? 'aucun changement' : '';
  save.onclick = () => docSave(id);
  f.append(left, el('span', 'sp'), txt, cancel, save);
}

// page d'un document : en-tête, état de chargement ou d'erreur
function docPage(p, id, title, intro, build) {
  const D = ST.docs[id];
  p.append(el('h4', '', title), el('p', 'ex', intro));
  if (!D || D.loading) { p.appendChild(el('p', 'dim', 'Chargement…')); return; }
  if (D.err) { p.appendChild(el('div', 't-red', 'Lecture impossible : ' + D.err + ' — utiliser « Éditer en YAML ».')); return; }
  build(D);
}

function confField(D, x) {
  const d = D.d;
  const touch = (v) => { d.v[x.key] = v; d.reset[x.key] = false; };
  let node;
  if (!x.editable) {
    node = el('span', 'dim', 'forme non reconnue' + (x.why ? ' (' + x.why + ')' : '') + ' : à modifier par « Éditer en YAML »');
    return node;
  }
  if (x.type === 'secret') {
    const w = el('span', 'stfont');
    const i = el('input', 'stin'); i.type = 'password'; i.autocomplete = 'off'; i.placeholder = x.set ? '•••••• (défini — saisir pour remplacer)' : '(non défini)';
    i.addEventListener('input', () => { d.token = i.value; d.tokenDel = false; stRenderFoot(); });
    w.appendChild(i);
    if (x.set) w.appendChild(stBtn('Retirer', () => { d.tokenDel = true; d.token = ''; i.value = ''; i.placeholder = 'sera retiré à l\'enregistrement'; stRenderFoot(); }));
    return w;
  }
  if (x.type === 'bool') node = stCheck(d.v[x.key], 'activé', (c) => { touch(c); stRenderFoot(); });
  else if (x.type === 'enum') node = stSelect(d.v[x.key], (x.options || []).map((o) => [o, o]), (v) => touch(v));
  else if (x.type === 'list') node = stArea(d.v[x.key], (v) => touch(v), 2);
  else if (x.type === 'int') { node = stInput(d.v[x.key], (v) => touch(v), '', 'w'); node.inputMode = 'numeric'; }
  else node = stInput(d.v[x.key], (v) => touch(v), x.type === 'intlist' ? 'nombres séparés par des espaces' : '');
  return node;
}

function stPageConf(p) {
  docPage(p, 'conf', 'Launcher', 'Réglages généraux du launcher, communs aux interfaces Linux et Windows.', (D) => {
    const bySet = {};
    for (const x of D.model.settings || []) (bySet[x.group] = bySet[x.group] || []).push(x);
    for (const g of D.model.groups || []) {
      const rows = [];
      for (const x of bySet[g.id] || []) {
        const hint = [x.help, x.set ? '' : 'défaut : ' + (x.type === 'secret' ? '(aucun)' : confShow(x))].filter(Boolean).join(' · ');
        const cell = el('div'); cell.appendChild(confField(D, x));
        if (x.editable && x.type !== 'secret' && x.set) {
          const r = stBtn('↺ défaut', () => { D.d.v[x.key] = confDisplay(x, x.default); D.d.reset[x.key] = true; stRenderPage(); stRenderFoot(); }, 'dim');
          r.title = 'retirer la ligne du fichier (valeur par défaut : ' + confShow(x) + ')'; cell.appendChild(r);
        }
        rows.push([x.label, cell, hint]);
      }
      if (rows.length) p.appendChild(stBox(g.label, stForm(rows)));
    }
    if ((D.model.unrecognized || []).length) {
      const b = el('div');
      b.append(el('p', 'note', 'Variables de config.conf écrites sous une forme non reconnue (boucle, eval, +=, valeur composée…) : elles sont conservées telles quelles et ne se modifient que dans l\'éditeur YAML.'), el('p', 'm', D.model.unrecognized.join(', ')));
      b.appendChild(stBtn('Éditer en YAML', () => stText('config.conf')));
      p.appendChild(stBox('Non modifiables ici', b));
    }
    if (!D.exists) p.appendChild(el('p', 'note', 'config.conf n\'existe pas encore : il sera créé à l\'enregistrement.'));
  });
}
function confShow(x) {
  const v = x.default;
  if (x.type === 'bool') return v && v !== '0' ? 'activé' : 'désactivé';
  if (Array.isArray(v)) return v.length ? v.join(' ') : '(vide)';
  return v == null || v === '' ? '(vide)' : String(v);
}

function stPageEnv(p) {
  docPage(p, 'env', 'Conteneurs — ajouts communs', 'Ajouté à l\'environnement de chaque conteneur, avant ce que demandent le template et le projet.', (D) => {
    const t = D.d.txt, f = [];
    f.push(['Paquets', stArea(t.packages, (v) => { t.packages = v; }, 4), 'un paquet par ligne']);
    f.push(['Paquets par gestionnaire', stArea(t.pbm, (v) => { t.pbm = v; }, 2), 'une ligne par gestionnaire : apt: git jq']);
    f.push(['setup', stArea(t.setup, (v) => { t.setup = v; }, 3), 'une commande par ligne, jouée à la création du conteneur']);
    f.push(['on_start', stArea(t.on_start, (v) => { t.on_start = v; }, 3), 'une commande par ligne, jouée à chaque démarrage']);
    p.appendChild(stBox('Outillage du launcher', stForm(f), el('span', 'dim', 'env.yaml (launcher)')));
  });
}

function stPageHub(p) {
  const D0 = ST.docs.hub;
  const title = 'Ce poste' + (D0 && D0.model ? ' — ' + D0.model.hub : '');
  docPage(p, 'hub', title, 'Réglages propres à cette machine. Les autres postes ne sont pas touchés.', (D) => {
    const m = D.model, d = D.d, t = d.txt, f = d.f;
    if (!m.present) p.appendChild(el('p', 'note', 'Aucune section pour ce poste dans backends.yaml : elle sera créée à l\'enregistrement.'));
    const eff = m.effective || {}, over = Object.keys(eff).filter((k) => eff[k]);
    if (over.length) p.appendChild(el('p', 't-yellow', '⚠ Surcharges propres au système (' + m.goos + ') : ' + over.map((k) => k + ' (' + eff[k] + ')').join(', ') + ' — la valeur commune ci-dessous est ignorée sur ce poste pour ces clés ; la surcharge se modifie par « Éditer en YAML ».'));
    const ovHint = (k) => (eff[k] ? '⚠ surchargé par le système ' + eff[k] : '');
    // affichage
    const inp = el('input', 'stin w'); inp.type = 'number'; inp.min = MIN_FONT; inp.max = MAX_FONT; inp.value = FONT_SIZE;
    inp.addEventListener('change', () => { setFontSize(inp.value); inp.value = FONT_SIZE; });
    const row = el('span', 'stfont');
    row.append(stBtn('−', () => { setFontSize(FONT_SIZE - 1); inp.value = FONT_SIZE; }), inp, stBtn('+', () => { setFontSize(FONT_SIZE + 1); inp.value = FONT_SIZE; }),
      el('span', 'dim', 'terminaux seulement · défaut ' + FONT_DEFAULT + ' · aussi par Ctrl+molette'));
    p.appendChild(stBox('Affichage', stForm([['Taille des caractères', row, 'mémorisée sur ce poste (desktop-ui.json), appliquée tout de suite, sans « Enregistrer »']])));
    // correspondance de chemins
    p.appendChild(stBox('Correspondance de chemins', stForm([['Chemins', stArea(t.paths, (v) => { t.paths = v; }, 3), 'un par ligne : chemin local => chemin dans le conteneur ' + ovHint('paths')]])));
    // navigateur
    const nav = stForm([
      ['Chemins du navigateur', stArea(t.browser_paths, (v) => { t.browser_paths = v; }, 3), 'un par ligne : chemin du conteneur => chemin de ce poste [| système] ' + ovHint('browser_paths')],
      ['Serveur web : écoute', stInput(f.listen, (v) => { f.listen = v; }, '127.0.0.1:8765'), ovHint('browser_server')],
      ['Serveur web : URL', stInput(f.url, (v) => { f.url = v; }, 'http://…'), 'adresse vue du navigateur'],
      ['Serveur web : autorisés', stArea(t.allow, (v) => { t.allow = v; }, 2), 'une adresse par ligne'],
      ['Navigateur (appariement)', stInput(f.browser_device, (v) => { f.browser_device = v; }, 'identifiant du navigateur'), ovHint('browser_device')],
    ]);
    p.appendChild(stBox('Navigateur', nav));
    // utilisateur distant, cache
    const ru = stForm([
      ['uid', stInput(f.uid, (v) => { f.uid = v; }, '', 'w'), ovHint('remote_user')], ['gid', stInput(f.gid, (v) => { f.gid = v; }, '', 'w')],
      ['Nom', stInput(f.name, (v) => { f.name = v; })], ['Dossier personnel', stInput(f.home, (v) => { f.home = v; })],
      ['Cache du hub', stInput(f.hub_cache, (v) => { f.hub_cache = v; }, '(par défaut)'), 'dossier du cache de ce poste ' + ovHint('hub_cache')],
    ]);
    p.appendChild(stBox('Utilisateur distant et cache', ru));
    const ovs = Object.keys(m.overrides || {});
    if (ovs.length) {
      const b = el('div');
      for (const os of ovs) b.appendChild(el('p', 'm', os + ' : ' + ((m.overrides[os].keys) || []).join(', ')));
      b.appendChild(el('p', 'note', 'Surcharges propres à un système : lecture seule ici, à modifier par « Éditer en YAML ».'));
      p.appendChild(stBox('Surcharges par système', b));
    }
  });
}

// ── Projet (#148, LOT-3.2) : projectSettings / projectApply ─────────────────
const PJ_LISTS = ['packages', 'setup', 'on_start', 'watch', 'local'];
const PJ_LABEL = { packages: 'Paquets', setup: 'setup', on_start: 'on_start', watch: 'watch', local: 'Dossiers local' };
const PJ_HINT = {
  packages: 'un paquet par ligne', setup: 'une commande par ligne, jouée à la création', on_start: 'une commande par ligne, jouée à chaque démarrage',
  watch: 'un chemin par ligne', local: 'un dossier du projet par ligne, gardé dans le conteneur (non partagé)',
};

function pjColorCss(n) { // indice tmux 256 → couleur CSS
  n = parseInt(n, 10);
  if (isNaN(n) || n < 0 || n > 255) return 'transparent';
  if (n < 16) return ['#000', '#800000', '#008000', '#808000', '#000080', '#800080', '#008080', '#c0c0c0', '#808080', '#f00', '#0f0', '#ff0', '#00f', '#f0f', '#0ff', '#fff'][n];
  if (n >= 232) { const g = 8 + (n - 232) * 10; return 'rgb(' + g + ',' + g + ',' + g + ')'; }
  const m = n - 16, c = (v) => (v ? 55 + v * 40 : 0);
  return 'rgb(' + c(Math.floor(m / 36)) + ',' + c(Math.floor(m / 6) % 6) + ',' + c(m % 6) + ')';
}
const pjPbmText = (m) => Object.keys(m || {}).sort().map((k) => k + ': ' + (m[k] || []).join(' ')).join('\n');
const pjMountsText = (a) => (a || []).map((x) => x.path + (x.ro === false ? ':rw' : '')).join('\n');
function pjParsePbm(t) {
  const o = {};
  for (const l of stLines(t)) { const i = l.indexOf(':'); if (i < 0) continue; const k = l.slice(0, i).trim(), v = l.slice(i + 1).split(/\s+/).filter(Boolean); if (k && v.length) o[k] = v; }
  return o;
}
function pjParseMounts(t) {
  return stLines(t).map((l) => (/:rw$/.test(l) ? { path: l.replace(/:rw$/, ''), ro: false } : { path: l.replace(/:ro$/, '') }));
}
const pjSorted = (o) => JSON.stringify(Object.keys(o).sort().map((k) => [k, o[k]]));

async function pjLoad(keepDraft) {
  const path = ST.project;
  if (!path || path === 'new') { ST.pj = null; return; }
  if (!keepDraft) ST.pj = { loading: true, path };
  try {
    const r = await call('projectSettings', { path });
    if (!ST.on || !ST.pj || ST.pj.path !== path) return;
    const e = r.env || {};
    const txt = {};
    for (const k of PJ_LISTS) txt[k] = (e[k] || []).join('\n');
    txt.pbm = pjPbmText(e.packages_by_manager); txt.mounts = pjMountsText(e.mounts);
    ST.pj = { path, loading: false, data: r, orig: { backend: r.backend || '', image: r.image || '', color: r.colorAuto ? '' : String(r.color || ''), env: { image: e.image || '', prefer: e.prefer || '', txt: stClone(txt) } },
      d: { backend: r.backend || '', image: r.image || '', color: r.colorAuto ? '' : String(r.color || ''), env: { image: e.image || '', prefer: e.prefer || '', txt } },
      merged: r.merged || [], mergedProblems: null, result: null, mtime: r.envMtime || '' };
  } catch (e) { ST.pj = { path, loading: false, err: e.message }; }
}

// changements d'env.yaml du projet (même forme que configApply)
function pjEnvChanges() {
  const p = ST.pj; if (!p || !p.d) return [];
  const o = p.orig.env, n = p.d.env, ch = [];
  const put = (k, nv, ov, empty) => {
    if (JSON.stringify(nv) === JSON.stringify(ov)) return;
    ch.push(empty ? { op: 'delete', at: [k] } : { op: 'set', at: [k], value: nv });
  };
  for (const k of ['image', 'prefer']) put(k, n[k].trim(), o[k], !n[k].trim());
  for (const k of PJ_LISTS) put(k, stLines(n.txt[k]), stLines(o.txt[k]), !stLines(n.txt[k]).length);
  const nm = pjParsePbm(n.txt.pbm), om = pjParsePbm(o.txt.pbm);
  if (pjSorted(nm) !== pjSorted(om)) ch.push(Object.keys(nm).length ? { op: 'set', at: ['packages_by_manager'], value: nm } : { op: 'delete', at: ['packages_by_manager'] });
  const nmo = pjParseMounts(n.txt.mounts), omo = pjParseMounts(o.txt.mounts);
  put('mounts', nmo, omo, !nmo.length);
  return ch;
}
function pjExecChanged() { const p = ST.pj; return !!(p && p.d && (p.d.backend !== p.orig.backend || p.d.image !== p.orig.image)); }
const pjColorChanged = () => !!(ST.pj && ST.pj.d && ST.pj.d.color !== ST.pj.orig.color);
const pjDirty = () => !!(ST.pj && ST.pj.d && (pjEnvChanges().length || pjExecChanged() || pjColorChanged()));

// aperçu fusionné en direct : le Go fusionne, rien n'est écrit
function pjPreview() {
  const p = ST.pj; if (!p || !p.d) return;
  clearTimeout(p.pvTimer);
  p.pvTimer = setTimeout(async () => {
    const ch = pjEnvChanges();
    try {
      const r = await call('projectSettings', { path: p.path, changes: ch.length ? ch : undefined });
      if (ST.pj !== p || !ST.on) return;
      p.merged = r.merged || []; p.mergedProblems = ch.length ? (r.problems || []) : null;
      pjRenderMerged(); stRenderFoot();
    } catch (e) { /* l'aperçu reste celui d'avant */ }
  }, 300);
}
function pjRenderMerged() {
  const m = $('pjmerged'); if (!m || !ST.pj) return;
  m.textContent = '';
  for (const l of ST.pj.merged) m.appendChild(el('div', 'mg mg-' + (l.from || 'none'), l.text || ' '));
}

function pjArea(key, rows) {
  const d = ST.pj.d.env.txt;
  return stArea(d[key], (v) => { d[key] = v; pjPreview(); }, rows);
}

function stPageProjet(p) {
  const pj = ST.pj;
  const name = ST.project ? ST.project.split(/[\\/]/).filter(Boolean).pop() : '';
  if (!ST.project || ST.project === 'new') {
    p.append(el('h4', '', 'Projet'), el('p', 'note', 'Sélectionnez d\'abord un projet dans le navigateur (ou clic droit sur un projet → « ⚙ Réglages du projet… »).'));
    return;
  }
  const h = el('h4', '', 'Projet — ' + name);
  if (pj && pj.data) h.appendChild(el('span', 'pill', pj.data.open ? 'ouvert' : 'fermé'));
  p.append(h, el('p', 'ex', 'Ce qui est propre à ce projet. Le reste vient du template (violet) et du launcher (bleu).'));
  if (!pj || pj.loading) { p.appendChild(el('p', 'dim', 'Chargement…')); return; }
  if (pj.err) { p.appendChild(el('div', 't-red', 'Lecture impossible : ' + pj.err + ' — utiliser « Éditer en YAML ».')); return; }
  const r = pj.data, d = pj.d, open = r.open;
  // Exécution
  const be = el('select', 'stin'); be.id = 'pjf-backend';
  const names = (r.backends || []).slice(); if (d.backend && !names.includes(d.backend)) names.push(d.backend);
  if (!d.backend) be.appendChild(Object.assign(el('option', '', '(non configuré)'), { value: '' }));
  for (const n of names) be.appendChild(Object.assign(el('option', '', n === 'host' ? 'host (hôte local)' : n), { value: n }));
  be.value = d.backend; be.disabled = open;
  be.addEventListener('change', () => { d.backend = be.value; stRenderFoot(); });
  const im = stInput(d.image, (v) => { d.image = v.trim(); }, 'image de base', 'w'); im.id = 'pjf-image'; im.disabled = open;
  const dl = el('datalist'); dl.id = 'pjimages'; for (const i of r.images || []) dl.appendChild(Object.assign(el('option'), { value: i }));
  im.setAttribute('list', 'pjimages');
  const rows = [['Backend', be, open ? 'projet ouvert : le fermer avant de changer de backend ou d\'image' : 'hôte, ou un backend conteneur ; modifiable projet fermé'],
    ['Image de base', im, 'ignorée pour le backend host'], ['Backend préféré', stInput(d.env.prefer, (v) => { d.env.prefer = v; pjPreview(); }, '(aucun)'), 'proposé aux autres postes (prefer, env.yaml)']];
  const sw = el('span', 'pjcolors'); sw.id = 'pjf-color';
  const mk = (val, label, css) => {
    const c = el('span', 'pjsw' + (d.color === val ? ' on' : '')); c.title = label; if (css) c.style.background = css; else c.textContent = 'auto';
    c.onclick = () => { d.color = val; for (const x of sw.children) x.classList.remove('on'); c.classList.add('on'); stRenderFoot(); };
    sw.appendChild(c);
  };
  mk('', 'couleur automatique', '');
  for (const c of r.palette || []) mk(String(c), 'couleur ' + c, pjColorCss(c));
  rows.push(['Couleur', sw, r.colorAuto && !d.color ? 'automatique (aucune couleur fixée)' : '']);
  const ex = stForm(rows); ex.appendChild(dl);
  p.appendChild(stBox('Exécution', ex, el('span', 'dim', 'projects.json · config.conf')));
  // Environnement
  const env = el('div');
  const f2 = [['Image', stInput(d.env.image, (v) => { d.env.image = v; pjPreview(); }, 'image du projet (surcharge le template)')]];
  for (const k of PJ_LISTS) f2.push([PJ_LABEL[k], pjArea(k, k === 'packages' ? 4 : 3), PJ_HINT[k]]);
  f2.push(['Paquets par gestionnaire', stArea(d.env.txt.pbm, (v) => { d.env.txt.pbm = v; pjPreview(); }, 2), 'une ligne par gestionnaire : apt: git jq']);
  f2.push(['Montages', stArea(d.env.txt.mounts, (v) => { d.env.txt.mounts = v; pjPreview(); }, 3), 'un chemin par ligne, lecture seule ; « :rw » en fin de ligne pour écrire']);
  env.appendChild(stForm(f2)); env.id = 'pjf-env';
  p.appendChild(stBox('Environnement du conteneur', env, el('span', 'dim', r.envPath ? r.envPath.split(/[\\/]/).slice(-2).join('/') + (r.envExists ? '' : ' (absent : créé à l\'enregistrement)') : '')));
  // Aperçu fusionné
  const mg = el('div', 'pjmerged'); mg.id = 'pjmerged';
  const leg = el('p', 'note', ''); leg.append(el('span', 'mg-template', '■ apporté par env-template.yaml'), ' (non modifiable ici) · ', el('span', 'mg-launcher', '■ ajouté par le launcher'), ' · le reste vient du projet.' + (r.hasTemplate ? '' : ' (aucun env-template.yaml pour ce projet)'));
  const box = el('div'); box.append(mg, leg);
  p.appendChild(stBox('Environnement fusionné — aperçu en lecture seule', box));
  pjRenderMerged();
  if (pj.focus) { const t = $(pj.focus); pj.focus = ''; if (t) { t.scrollIntoView({ block: 'center' }); const f = t.matches('input,select,textarea') ? t : t.querySelector('input,select,textarea'); if (f && !f.disabled) f.focus(); } }
}

function pjFoot(f) {
  const pj = ST.pj;
  const left = el('div', 'stmsg');
  const dirty = pjDirty();
  const probs = pj && pj.res && pj.res.problems ? pj.res.problems : (pj && pj.mergedProblems ? pj.mergedProblems : (pj && pj.data ? pj.data.problems || [] : []));
  const errs = probs.filter((x) => x.severity === 'error').length;
  const res = pj && pj.res;
  if (res && res.conflict) {
    left.append(el('span', 't-orange', 'env.yaml a changé sur le disque depuis son ouverture. '));
    const rel = el('button', 'btn sm', 'Recharger (perdre mes modifications)'); rel.onclick = async () => { await pjLoad(); stRender(); };
    const ow = el('button', 'btn sm danger', 'Écraser avec ma version'); ow.onclick = () => { pj.mtime = res.mtime; pj.res = null; pjSave(); };
    left.append(rel, ow);
  } else if (res && res.msg) {
    left.appendChild(el('span', 't-red', '✗ ' + res.msg));
  } else if (probs.length) {
    for (const x of probs) left.appendChild(el('div', 'ceprob ' + (x.severity === 'error' ? 't-red' : 't-yellow'), (x.severity === 'error' ? '✗ ' : '⚠ ') + (x.line ? 'ligne ' + x.line + ' : ' : '') + x.message));
  } else if (res && res.saved) {
    left.appendChild(el('span', 't-green', '✓ Enregistré. ' + (res.note || '')));
  } else if (!pj || pj.loading) {
    left.appendChild(el('span', 'dim', 'Chargement…'));
  } else left.appendChild(el('span', 't-green', '✓ valide'));
  const txt = el('button', 'btn', 'Éditer env.yaml en YAML'); txt.onclick = () => stText('project-env');
  txt.disabled = !ST.project || ST.project === 'new';
  const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = () => stClose();
  const save = el('button', 'btn pri', 'Enregistrer'); save.id = 'stsave';
  save.disabled = !dirty || errs > 0 || !(pj && pj.d);
  save.title = errs ? 'Corriger les erreurs avant d\'enregistrer' : !dirty ? 'aucun changement' : '';
  save.onclick = () => pjSave();
  f.append(left, el('span', 'sp'), txt, cancel, save);
}

async function pjSave() {
  const pj = ST.pj; if (!pj || !pj.d) return;
  const arg = { path: pj.path };
  if (pjExecChanged()) { arg.backend = pj.d.backend; if (pj.d.backend !== 'host') arg.image = pj.d.image; }
  if (pjColorChanged()) arg.color = pj.d.color;
  const ch = pjEnvChanges();
  if (ch.length) arg.env = { mtime: pj.mtime, changes: ch };
  let r;
  try { r = await call('projectApply', arg); } catch (e) { return; }
  if (!r.ok && ((r.env && r.env.saved) || r.colorSaved)) { // résultat partiel : env.yaml et/ou couleur écrits, la suite a échoué — relire (nouvelle date) et dire quoi
    const done = [(r.env && r.env.saved) ? 'env.yaml' : '', r.colorSaved ? 'couleur' : ''].filter(Boolean).join(' et ');
    const msg = (r.msg || 'refusé') + ' — ' + done + ' enregistré' + (done.includes(' et ') ? 's' : '') + '.';
    await pjLoad();
    if (ST.pj) ST.pj.res = { msg };
    stRender(); return;
  }
  if (!r.ok) {
    pj.res = r.env ? (r.env.conflict ? { conflict: true, mtime: r.env.mtime } : { problems: r.env.problems || [] }) : { msg: r.msg || 'refusé' };
    stRenderFoot(); return;
  }
  const keep = pj.focus;
  await pjLoad();
  if (ST.pj) ST.pj.res = { saved: true, note: r.note || '' };
  stRender();
}

$('cfgbtn').title = 'Réglages (Ctrl+,)';
