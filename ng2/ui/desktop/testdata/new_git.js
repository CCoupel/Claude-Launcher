// Simulation (node) de #215 : URL git, nom proposé, ligne d'auth, avancement du job. Usage : node new_git.js app.js
const fs = require('fs'), vm = require('vm');
const src = fs.readFileSync(process.argv[2], 'utf8');
function grab(sig) {
  const at = src.indexOf(sig); if (at < 0) { console.log('KO : ' + sig + ' absente'); process.exit(1); }
  const open = src.indexOf('{', src.indexOf(')', at)); let d = 0;
  for (let k = open; k < src.length; k++) { if (src[k] === '{') d++; else if (src[k] === '}' && --d === 0) return src.slice(at, k + 1); }
}
const ctx = {}; vm.createContext(ctx);
vm.runInContext(['gitUrlKind', 'nameFromUrl', 'authLine', 'jobView', 'jobGone'].map((n) => grab('function ' + n + '(')).join('\n') + '\nglobalThis.t = { gitUrlKind, nameFromUrl, authLine, jobView, jobGone };', ctx);
const { gitUrlKind, nameFromUrl, authLine, jobView, jobGone } = ctx.t;
let ko = 0; const eq = (n, a, b) => { if (JSON.stringify(a) !== JSON.stringify(b)) { ko++; console.log('KO', n, JSON.stringify(a), '!=', JSON.stringify(b)); } };
for (const [u, k] of [['', ''], ['https://github.com/a/b.git', 'https'], ['git@github.com:a/b.git', 'ssh'], ['ssh://git@h/a/b', 'ssh'],
  ['file:///x', 'bad'], ['/srv/repo', 'bad'], ['--upload-pack=x', 'bad'], ['http://h/a', 'bad'], ['https://h/a b', 'bad'], ['  https://h/a/b ', 'https']]) eq('kind ' + u, gitUrlKind(u), k);
eq('nom .git', nameFromUrl('git@github.com:CCoupel/supervision-k8s.git'), 'supervision-k8s');
eq('nom https', nameFromUrl('https://github.com/a/b/'), 'b');
eq('nom vide', nameFromUrl(''), '');
const au = { token: true, sshKey: { k8s: true } };
eq('https jeton', authLine(au, 'https://h/a/b', '').ok, true);
eq('https sans jeton', authLine({ token: false }, 'https://h/a/b', '').ok, false);
eq('ssh backend', authLine(au, 'git@h:a/b', 'k8s').text, '🔑 clé ssh du backend ✓');
eq('ssh sans clé', authLine(au, 'git@h:a/b', 'autre').ok, false);
eq('ssh poste', authLine({ sshKey: { host: true } }, 'git@h:a/b', '').ok, true);
eq('pas de secret', /ghp_|BEGIN/.test(JSON.stringify(authLine({ token: 'ghp_SECRET' }, 'https://h/a/b', ''))), false);
eq('job', jobView({ step: 'réception', pct: 43.4, state: 'running' }), { state: 'running', pct: 43, label: 'réception — 43 %', msg: '' });
for (const st of ['préparation', 'résolution des deltas', 'extraction', 'terminé']) eq('étape ' + st, jobView({ step: st, pct: 10 }).label, st + ' — 10 %');
eq('sans étape', jobView({ pct: 0 }).label, 'clonage — 0 %');
eq('gone : jamais manqué', jobGone(false, null, 5000, 3000), false);
eq('gone : vu puis disparu', jobGone(true, 1000, 1001, 3000), true);
eq('gone : jamais vu, dans le délai', jobGone(false, 1000, 3000, 3000), false);
eq('gone : jamais vu, délai écoulé', jobGone(false, 1000, 4000, 3000), true);
eq('job borné', jobView({ pct: 250 }).pct, 100);
eq('job échec', jobView({ state: 'failed', msg: 'refusé' }).msg, 'refusé');
eq('job absent', jobView(null), null);
console.log(ko ? ko + ' KO' : 'OK'); process.exit(ko ? 1 : 0);
