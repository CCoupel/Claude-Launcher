// ── Fenêtre « Réglages » (#118, LOT-3.1) ────────────────────────────────────
// Configuration structurée du launcher, selon la maquette
// docs/mockup/v6.1.0/ui/config_ui__interface_structuree.html (A : fenêtre
// modale large à rubriques). Rubriques Backends, Pools, Défauts (backends.yaml)
// lues par configModel et écrites par configApply (liste de changements : les
// commentaires du fichier sont conservés côté Go) ; les autres rubriques
// renvoient pour l'instant à l'éditeur de texte (« Éditer en texte »).
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

async function openSettings(project, page) {
  ST.project = project || '';
  if (page) ST.page = page;
  ST.result = null; ST.test = null;
  await stLoad();
  openModal((b) => {
    b.classList.add('wide');
    b.innerHTML = '';
    const head = el('div', 'cehead');
    const ttl = el('h2', '', 'Réglages'); const path = el('span', 'cepath'); path.id = 'stpath';
    const txt = el('button', 'btn sm', 'Fichiers en texte…'); txt.onclick = () => stText('');
    const x = el('button', 'btn sm', '✕'); x.onclick = () => stClose();
    head.append(ttl, path, el('span', 'sp'), txt, x);
    const body = el('div', 'stbody');
    const rail = el('div', 'strail'); rail.id = 'strail';
    const pg = el('div', 'stpg'); pg.id = 'stpg';
    body.append(rail, pg);
    const foot = el('div', 'stfoot'); foot.id = 'stfoot';
    b.append(head, body, foot);
  });
  ST.on = true;
  stRender();
  stPoll();
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
function stBuildChanges() {
  const d = ST.draft, o = ST.orig, ch = [];
  if (!d) return ch;
  const live = new Set(d.backends.map((b) => b._orig).filter(Boolean));
  for (const ob of o.backends) if (!ob.implicit && !live.has(ob.name)) ch.push({ op: 'delete', at: ['backends', ob.name] });
  for (const b of d.backends) {
    if (b.implicit) continue;
    const ob = b._orig ? o.backends.find((x) => x.name === b._orig) : null;
    const val = stBackendValue(b);
    if (!ob) { ch.push({ op: 'set', at: ['backends', b.name], value: val }); continue; }
    const same = JSON.stringify(val) === JSON.stringify(stBackendValue(ob));
    if (b.name !== ob.name) ch.push({ op: 'rename', at: ['backends', ob.name], value: b.name });
    if (!same) ch.push({ op: 'set', at: ['backends', b.name], value: val });
  }
  const livep = new Set(d.pools.map((p) => p._orig).filter(Boolean));
  for (const op of o.pools) if (!livep.has(op.name)) ch.push({ op: 'delete', at: ['pools', op.name] });
  for (const p of d.pools) {
    const op = p._orig ? o.pools.find((x) => x.name === p._orig) : null;
    const val = { hosts: p.hosts || [] }; if (p.drain && p.drain.length) val.drain = p.drain;
    if (!op) { ch.push({ op: 'set', at: ['pools', p.name], value: val }); continue; }
    const same = JSON.stringify(val) === JSON.stringify({ hosts: op.hosts || [], ...(op.drain && op.drain.length ? { drain: op.drain } : {}) });
    if (p.name !== op.name) ch.push({ op: 'rename', at: ['pools', op.name], value: p.name });
    if (!same) ch.push({ op: 'set', at: ['pools', p.name], value: val });
  }
  for (const k of ['backend', 'image']) {
    const a = d.defaults[k] || '', b = (o.defaults || {})[k] || '';
    if (a !== b) ch.push(a ? { op: 'set', at: ['defaults', k], value: a } : { op: 'delete', at: ['defaults', k] });
  }
  if (JSON.stringify(d.images) !== JSON.stringify(o.images || [])) {
    ch.push(d.images.length ? { op: 'set', at: ['images'], value: d.images } : { op: 'delete', at: ['images'] });
  }
  return ch;
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
  } else {
    left.appendChild(el('span', 't-green', '✓ valide'));
  }
  const txt = el('button', 'btn', 'Éditer en texte'); txt.onclick = () => stText(ST_KIND[ST.page]);
  const cancel = el('button', 'btn', 'Annuler'); cancel.onclick = () => stClose();
  const save = el('button', 'btn pri', 'Enregistrer'); save.id = 'stsave';
  save.disabled = !ch.length || errs > 0 || !ST.draft || !stEditable();
  save.title = errs ? 'Corriger les erreurs avant d\'enregistrer' : !ch.length ? 'aucun changement' : '';
  save.onclick = () => stSave();
  f.append(left, el('span', 'sp'), txt, cancel, save);
}

const stEditable = () => ST.page === 'backends' || ST.page === 'pools' || ST.page === 'defauts';
const stDirty = () => stBuildChanges().length > 0;

async function stSave() {
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
  const project = ST.project;
  stClose(true);
  configEditor(project, kind || '');
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
  if (!ST.draft && stEditable()) { p.appendChild(el('div', 't-red', 'Lecture impossible : ' + ((ST.loadProblems[0] || {}).message || 'erreur') + ' — utiliser « Éditer en texte ».')); return; }
  const fn = { backends: stPageBackends, pools: stPagePools, defauts: stPageDefaults, launcher: stPageText, conteneurs: stPageText, poste: stPagePoste, projet: stPageProjet }[ST.page];
  fn(p);
  p.scrollTop = top;
}

// ── Backends ───────────────────────────────────────────────────────────────
function stStateCell(name) {
  const s = ST.states[name];
  if (!s) return el('span', 'dim', '…');
  if (s.pending) return el('span', 'dim', '… test en cours');
  return el('span', s.ok ? 't-green' : 't-yellow', (s.ok ? '● ' : '⚠ ') + (s.text || ''));
}
function stAddr(b) { return b.host || (b.hosts && b.hosts.length ? b.hosts.join(', ') : '') || (b.type === 'k8s' ? [b.context, b.namespace].filter(Boolean).join(' / ') : (b.type === 'host' ? '' : '(local)')); }

function stPageBackends(p) {
  const d = ST.draft;
  p.append(el('h4', '', 'Backends'), el('p', 'ex', 'Où s\'exécutent les projets : sur l\'hôte, dans un conteneur local ou distant, ou dans un pod Kubernetes.'));
  const split = el('div', 'stsplit'); p.appendChild(split);
  const left = el('div'); split.appendChild(left);
  const t = el('table', 'sttable'); const hr = el('tr');
  for (const h of ['Nom', 'Type', 'Adresse', 'Pool', 'État']) hr.appendChild(el('th', '', h));
  t.appendChild(hr);
  if (ST.sel.backend >= d.backends.length) ST.sel.backend = Math.max(0, d.backends.length - 1);
  d.backends.forEach((b, i) => {
    const r = el('tr', i === ST.sel.backend ? 'on' : '');
    const n = el('td', 'm', b.name || '(sans nom)');
    if (d.defaults.backend && d.defaults.backend === b.name) n.appendChild(el('span', 'pill', 'défaut'));
    r.append(n, el('td', '', b.type), el('td', 'm', stAddr(b)), el('td', 'm', b.pool || ''), el('td'));
    r.lastChild.appendChild(b._orig ? stStateCell(b._orig) : el('span', 'dim', 'nouveau'));
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
  // navigateur : affiché, modifiable en texte (rubrique « Ce poste » au sous-lot suivant)
  const n = el('div');
  const bs = (d.defaults.browser_server) || null, bp = (d.defaults.browser_paths) || [];
  n.appendChild(el('p', 'note', 'Défaut de tous les postes, utilisé quand « Ce poste » ne définit rien. Modifiable pour l\'instant par « Éditer en texte ».'));
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
function stPageText(p) {
  const t = { launcher: ['Launcher', 'config.conf', 'Réglages généraux du launcher : racines, options de claude, teams, masquage des inactifs, palette…'], conteneurs: ['Conteneurs', 'env.yaml du launcher', 'Paquets, setup et on_start ajoutés à tous les conteneurs.'] }[ST.page];
  p.append(el('h4', '', t[0]), el('p', 'ex', t[2]), el('p', 'note', 'Cette rubrique n\'a pas encore de formulaire : elle se modifie dans l\'éditeur de texte (' + t[1] + ').'));
  p.appendChild(stBtn('Éditer en texte', () => stText(ST_KIND[ST.page])));
}

function stPagePoste(p) {
  p.append(el('h4', '', 'Ce poste'), el('p', 'ex', 'Réglages propres à cette machine. Les autres postes ne sont pas touchés.'));
  const body = el('div');
  const inp = el('input', 'stin w'); inp.type = 'number'; inp.min = MIN_FONT; inp.max = MAX_FONT; inp.value = FONT_SIZE;
  inp.addEventListener('change', () => { setFontSize(inp.value); inp.value = FONT_SIZE; });
  const row = el('span', 'stfont');
  row.append(stBtn('−', () => { setFontSize(FONT_SIZE - 1); inp.value = FONT_SIZE; }), inp, stBtn('+', () => { setFontSize(FONT_SIZE + 1); inp.value = FONT_SIZE; }),
    el('span', 'dim', 'terminaux seulement · défaut ' + FONT_DEFAULT + ' · aussi par Ctrl+molette'));
  body.appendChild(stForm([['Taille des caractères', row]]));
  body.appendChild(el('p', 'note', 'Mémorisée sur ce poste (appliquée tout de suite, sans « Enregistrer »). Correspondance de chemins, navigateur et cache du hub : pas encore de formulaire, voir « Éditer en texte » (backends.yaml → hubs).'));
  p.appendChild(stBox('Affichage', body));
}

function stPageProjet(p) {
  p.append(el('h4', '', 'Projet'), el('p', 'ex', 'Réglages du projet sélectionné : backend, environnement du conteneur, couleur.'));
  const ok = ST.project && ST.project !== 'new';
  p.appendChild(el('p', 'note', ok ? 'Pas encore de formulaire : l\'environnement du projet se modifie dans l\'éditeur de texte (env.yaml du projet).' : 'Sélectionnez d\'abord un projet dans le navigateur.'));
  if (ok) p.appendChild(stBtn('Éditer env.yaml en texte', () => stText('project-env')));
}

$('cfgbtn').title = 'Réglages (Ctrl+,)';
