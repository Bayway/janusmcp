package webui

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>JanusMCP — Control Panel</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.5 -apple-system, system-ui, sans-serif; max-width: 880px; margin: 2rem auto; padding: 0 1rem; }
  h1 { font-size: 1.4rem; margin-bottom: .2rem; }
  .sub { opacity:.7; margin-top:0; }
  table { width: 100%; border-collapse: collapse; margin: 1rem 0; }
  th, td { text-align: left; padding: .55rem .5rem; border-bottom: 1px solid rgba(128,128,128,.25); }
  th { font-size: .8rem; text-transform: uppercase; letter-spacing: .03em; opacity: .6; }
  .badge { font-size: .78rem; padding: .15rem .5rem; border-radius: 999px; white-space: nowrap; }
  .ready { background: rgba(40,180,90,.18); color: #1c8c47; }
  .needs-login { background: rgba(230,160,30,.18); color: #b9791b; }
  .needs-secret { background: rgba(220,80,80,.18); color: #c0392b; }
  button { font: inherit; padding: .3rem .7rem; border-radius: 8px; border: 1px solid rgba(128,128,128,.4); background: transparent; cursor: pointer; }
  button.primary { background: #6c5ce7; color: #fff; border-color: #6c5ce7; }
  button:disabled { opacity: .5; cursor: default; }
  .card { border: 1px solid rgba(128,128,128,.25); border-radius: 12px; padding: 1rem 1.2rem; margin-top: 1.5rem; }
  .row { display: flex; gap: .6rem; flex-wrap: wrap; align-items: center; margin-top:.6rem; }
  input, select { font: inherit; padding: .35rem .5rem; border-radius: 8px; border: 1px solid rgba(128,128,128,.4); background: transparent; }
  input { min-width: 14rem; }
  .muted { opacity: .65; font-size: .85rem; }
  #toast { position: fixed; bottom: 1rem; left: 50%; transform: translateX(-50%); background: #222; color:#fff; padding:.6rem 1rem; border-radius: 8px; opacity:0; transition:.3s; pointer-events:none; }
  #toast.show { opacity: 1; }
</style>
</head>
<body>
  <h1>JanusMCP</h1>
  <p class="sub">Local control panel — manage accounts, logins and secrets.</p>

  <table>
    <thead><tr><th>Account</th><th>Service</th><th>Transport</th><th>Status</th><th></th></tr></thead>
    <tbody id="accounts"><tr><td colspan="5" class="muted">Loading…</td></tr></tbody>
  </table>

  <div class="card">
    <strong>Add account</strong>
    <div class="row">
      <select id="tmpl"></select>
      <input id="newid" placeholder="account id (e.g. supabase_acme)"/>
      <button class="primary" onclick="addAccount()">Add</button>
    </div>
    <p class="muted" id="tmpldesc"></p>
  </div>

  <div id="toast"></div>

<script>
const $ = (s) => document.querySelector(s);
function toast(msg) { const t = $('#toast'); t.textContent = msg; t.classList.add('show'); setTimeout(()=>t.classList.remove('show'), 2600); }

async function api(path, body) {
  const opt = body ? { method:'POST', headers:{'content-type':'application/json'}, body: JSON.stringify(body) } : {};
  const r = await fetch(path, opt);
  const j = await r.json().catch(()=>({}));
  if (!r.ok) throw new Error(j.error || ('HTTP '+r.status));
  return j;
}

function badge(status) {
  const label = { 'ready':'ready', 'needs-login':'needs login', 'needs-secret':'needs secret' }[status] || status;
  return '<span class="badge '+status+'">'+label+'</span>';
}

async function load() {
  const data = await api('/api/accounts');
  const tb = $('#accounts');
  if (!data.accounts.length) { tb.innerHTML = '<tr><td colspan="5" class="muted">No accounts yet — add one below.</td></tr>'; return; }
  tb.innerHTML = '';
  for (const a of data.accounts) {
    const tr = document.createElement('tr');
    let action = '';
    if (a.status === 'needs-login' || (a.transport==='http' && a.auth==='oauth')) {
      action = '<button onclick="login(\'' + a.id + '\', this)">Login</button>';
    } else if (a.status === 'needs-secret' && a.missing && a.missing.length) {
      action = '<button onclick="setSecret(\'' + a.missing[0] + '\', this)">Set secret</button>';
    }
    tr.innerHTML = '<td><strong>'+a.id+'</strong><div class="muted">'+a.label+'</div></td>' +
                   '<td>'+a.service+'</td><td>'+a.transport+'</td><td>'+badge(a.status)+'</td><td>'+action+'</td>';
    tb.appendChild(tr);
  }
}

async function login(id, btn) {
  btn.disabled = true; btn.textContent = 'Opening browser…';
  try { await api('/api/login', { id }); toast('Logged in: '+id); }
  catch (e) { toast('Login failed: '+e.message); }
  btn.disabled = false; btn.textContent = 'Login';
  load();
}

async function setSecret(name, btn) {
  const value = prompt('Paste the secret for "'+name+'" (e.g. a Personal Access Token):');
  if (!value) return;
  try { await api('/api/secret', { name, value }); toast('Saved: '+name); }
  catch (e) { toast('Failed: '+e.message); }
  load();
}

async function loadTemplates() {
  const t = await api('/api/templates');
  const sel = $('#tmpl');
  sel.innerHTML = '';
  for (const x of t) { const o = document.createElement('option'); o.value = x.name; o.textContent = x.name; o.dataset.desc = x.description; sel.appendChild(o); }
  const upd = () => { const o = sel.selectedOptions[0]; $('#tmpldesc').textContent = o ? o.dataset.desc : ''; };
  sel.onchange = upd; upd();
}

async function addAccount() {
  const template = $('#tmpl').value, id = $('#newid').value.trim();
  try { const r = await api('/api/add', { template, id }); toast('Added: '+r.id); $('#newid').value=''; load(); }
  catch (e) { toast('Failed: '+e.message); }
}

loadTemplates(); load();
</script>
</body>
</html>`
