// Simulation (node) : une fiche backend reconstruite par stPoll / stRenderPage pendant la lecture du coffre
// ne doit pas laisser l'encadré sur « lecture du coffre… » (#193, #194). Usage : node st_refresh.js settings.js
const fs = require('fs'), vm = require('vm');
const doc = { byId: {} };
class N {
  constructor(t) { this.tag = t; this.children = []; this.className = ''; this._id = ''; this._tc = ''; this.classList = { add: (c) => { this.className += ' ' + c; } }; }
  set id(v) { this._id = v; doc.byId[v] = this; } get id() { return this._id; }
  append(...a) { for (const x of a) this.children.push(typeof x === 'string' ? { text: () => x } : x); }
  appendChild(x) { this.append(x); return x; }
  addEventListener() {} contains() { return false; }
  set textContent(v) { if (v === '') this.children = []; this._tc = v; } get textContent() { return this._tc; }
  text() { return this._tc + this.children.map((c) => c.text()).join(' '); }
}
const pending = [];
const ctx = {
  document: { createElement: (t) => new N(t) }, console, window: {}, setTimeout, setInterval() {}, confirm: () => true,
  el: (t, c, x) => { const n = new N(t); if (c) n.className = c; if (x != null) n.append(String(x)); return n; },
  $: (id) => doc.byId[id] || (/box$/.test(id) ? undefined : new N('x')),
  call: (m) => new Promise((r) => pending.push(() => r(m === 'backendSecretGet'
    ? { hasKey: true, publicKey: 'ssh-ed25519 X', fingerprint: 'SHA256:z', hasKubeconfig: true, kubeContexts: ['k8s'], knownHosts: [] } : {}))),
};
vm.createContext(ctx);
vm.runInContext(fs.readFileSync(process.argv[2], 'utf8') + '\nglobalThis.__t = { ST, stKubeBox, stSshBox };', ctx);
const { ST, stKubeBox, stSshBox } = ctx.__t;
(async () => {
  for (const [kind, fn, type] of [['kube', stKubeBox, 'k8s'], ['ssh', stSshBox, 'ssh']]) {
    const b = { name: 'b1', type, _orig: {} };
    ST.draft = { backends: [b] }; ST.sel = { backend: 0 };
    fn(b);                      // 1er rendu : lecture du coffre lancée
    const second = fn(b);       // stPoll : fiche reconstruite avant la réponse
    pending.splice(0).forEach((f) => f());
    await new Promise((r) => setTimeout(r, 10));
    const cur = ctx.$('st' + kind + 'box');
    if (cur !== second || /lecture du coffre/.test(cur.text())) { console.log('KO ' + kind + ' : ' + cur.text()); process.exit(1); }
    console.log('OK ' + kind);
  }
})();
