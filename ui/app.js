const $ = id => document.getElementById(id);
let files = [], selectedFile = null, selectedUnit = null, source = '';

async function getJSON(url, options) {
  const res = await fetch(url, options);
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

async function loadTree() {
  try {
    const data = await getJSON('/api/tree');
    files = data.files;
    $('provider').textContent = data.provider && data.model ? `${data.provider} · ${data.model}` : 'Structural mode · configure AI to explain';
    $('budget').textContent = `Input budget: ${data.used_input_tokens}/${data.session_input_budget}`;
    $('count').textContent = `${files.length} files`;
    renderFiles();
    if (selectedFile && !files.some(f => f.path === selectedFile)) selectedFile = null;
    if (!selectedFile && files.length) await openFile(files.find(f => f.path.endsWith('main.tf'))?.path || files[0].path);
  } catch (err) { $('file-list').textContent = err.message; }
}

function renderFiles() {
  const filter = $('filter').value.toLowerCase();
  $('file-list').replaceChildren();
  for (const file of files.filter(f => f.path.toLowerCase().includes(filter))) {
    const button = document.createElement('button');
    button.className = 'file-button' + (file.path === selectedFile ? ' active' : '');
    button.textContent = file.path;
    button.title = file.path;
    button.addEventListener('click', () => openFile(file.path));
    $('file-list').append(button);
  }
}

async function openFile(path) {
  try {
    const data = await getJSON('/api/file?path=' + encodeURIComponent(path));
    selectedFile = path; selectedUnit = null; source = data.source;
    $('filename').textContent = path;
    $('filekind').textContent = `${data.kind} · ${data.units.length} readable blocks`;
    $('unitlabel').textContent = 'Select a block or function';
    renderFiles();
    renderCode();
    $('units').replaceChildren();
    for (const unit of data.units) {
      const button = document.createElement('button');
      button.className = 'unit'; button.textContent = unit.name;
      button.title = `${unit.start}–${unit.end}`;
      button.addEventListener('click', () => selectUnit(unit));
      $('units').append(button);
    }
    $('detail').replaceChildren();
    const p = document.createElement('p'); p.className = 'muted';
    p.textContent = 'Choose a block above to inspect its source and request an explanation.';
    $('detail').append(p);
    if (data.units.length === 1) selectUnit(data.units[0]);
  } catch (err) { $('detail').textContent = err.message; }
}

function renderCode() {
  $('code').replaceChildren();
  source.split('\n').forEach((text, i) => {
    const line = document.createElement('div'); line.className = 'line';
    if (selectedUnit && i + 1 >= selectedUnit.start && i + 1 <= selectedUnit.end) line.classList.add('selected');
    const number = document.createElement('span'); number.className = 'line-number'; number.textContent = i + 1;
    const content = document.createElement('span'); content.className = 'line-content'; content.textContent = text || ' ';
    line.append(number, content); $('code').append(line);
  });
}

function selectUnit(unit) {
  selectedUnit = unit;
  $('unitlabel').textContent = `${unit.kind} · lines ${unit.start}–${unit.end}`;
  for (const button of $('units').children) button.classList.toggle('active', button.textContent === unit.name);
  renderCode();
  $('code').querySelector('.line.selected')?.scrollIntoView({block:'center'});
  $('detail').replaceChildren();
  const title = document.createElement('h3'); title.textContent = unit.name;
  const meta = document.createElement('div'); meta.className = 'meta'; meta.textContent = `Source: ${selectedFile}:${unit.start}–${unit.end}`;
  const note = document.createElement('p'); note.className = 'muted';
  note.textContent = unit.kind === 'module' ? 'This module call connects the current stack to a reusable building block.' : `The selected ${unit.kind} is anchored to the highlighted source. AI explanation is optional.`;
  $('detail').append(title, meta, note);
  for (const link of unit.links || []) {
    const button = document.createElement('button'); button.className = 'link';
    button.textContent = `Open local module → ${link}`;
    button.addEventListener('click', () => {
      const match = files.find(f => f.path === `${link}/main.tf`) || files.find(f => f.path.startsWith(`${link}/`) && f.kind === 'terraform');
      if (match) openFile(match.path); else button.textContent = 'Local module not indexed';
    });
    $('detail').append(button);
  }
  const action = document.createElement('button'); action.className = 'action'; action.textContent = 'Explain this block with AI';
  action.addEventListener('click', () => explain(unit, action)); $('detail').append(action);
  const warning = document.createElement('p'); warning.className = 'warning';
  warning.textContent = 'Clicking Explain sends the highlighted source to your configured provider. Review it before proceeding.';
  $('detail').append(warning);
}

async function explain(unit, button) {
  const snippet = source.split('\n').slice(unit.start - 1, unit.end).join('\n');
  if (!window.confirm(`Send the highlighted ${snippet.length} characters from ${selectedFile} to your configured AI provider?`)) return;
  button.disabled = true; button.textContent = 'Explaining…';
  try {
    const result = await getJSON('/api/explain', {
      method:'POST', headers:{'Content-Type':'application/json'},
      body:JSON.stringify({path:selectedFile, unit_id:unit.id, approved:true})
    });
    if (selectedUnit?.id !== unit.id) return;
    const prose = document.createElement('div'); prose.className = 'explanation'; prose.textContent = result.text;
    button.replaceWith(prose);
    const usage = document.createElement('p'); usage.className = 'meta';
    usage.textContent = result.cached ? 'Cached explanation · no model call' : `${result.input_tokens} input · ${result.output_tokens} output tokens`;
    prose.after(usage);
    loadTree();
  } catch (err) { button.textContent = 'Try explanation again'; const msg = document.createElement('p'); msg.className='warning'; msg.textContent=err.message; button.after(msg); }
  finally { button.disabled = false; }
}

$('filter').addEventListener('input', renderFiles);
$('refresh').addEventListener('click', async () => { selectedFile = null; await loadTree(); });
loadTree();
