'use strict';
const $ = id => document.getElementById(id);
const state = { files: [], overview: null, commands: [], path: null, unit: null, source: '', sourceLines: [], highlighted: null, codeWindow: 0, entry: null, orientation: null, sequence: 0, history: [], cursor: -1, tour: null, review: null, selection: null, changes: null, choices: [], choice: 0, pending: null, read: new Set(), sidebar: 'changes', diffMode: true, changeView: null, stop: 0, paused: false, prepared: null, provider: '', model: '', request: null };
const el = (tag, text, className) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; if (className) n.className = className; return n; };
function notice(text = '') { $('notice').textContent = text; }
async function request(url, options = {}) {
  const response = await fetch(url, options);
  const data = await response.json();
  if (!response.ok) { const error = new Error(data.error || `HTTP ${response.status}`); error.status = response.status; throw error; }
  return data;
}
const post = (url, body) => request(url, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)});
function sourceLabel(p) { return `${p.path}${p.start ? ':' + p.start + (p.end > p.start ? '–' + p.end : '') : ''}`; }
function provenance(parent, p) { parent.append(el('small', `${p.provider} · ${sourceLabel(p)}${p.detail ? ' · ' + p.detail : ''}`, 'meta')); }
function destination(node, reason, p, open = navigate) {
  const button = el('button', undefined, 'destination'); button.append(el('strong', node.name), el('small', `${node.path}:${node.start || 1} · ${reason}`));
  if (p) button.title = `${p.provider}: ${p.detail} (${sourceLabel(p)})`;
  button.addEventListener('click', () => open(node)); return button;
}
function card(title, id) { const box = el('div', undefined, 'card'); if (id) box.id = id; box.append(el('h3', title)); return box; }
async function loadRepository() {
  const [tree, overview, commands] = await Promise.all([request('/api/tree'), request('/api/overview'), request('/api/commands')]);
  state.files = tree.files; state.overview = overview; state.commands = commands; state.provider = tree.provider; state.model = tree.model;
  $('provider').textContent = tree.provider && tree.model ? `AI available · ${tree.provider} / ${tree.model}` : 'Local analysis · AI optional';
  $('budget').textContent = tree.provider ? `${tree.used_input_tokens} / ${tree.session_input_budget} input tokens` : '';
  renderFiles(); renderHelp();
}
function renderFiles() {
  const reviewing = !!state.review, stops = reviewing && state.sidebar === 'changes';
  $('side-tabs').hidden = !reviewing; $('side-title').textContent = reviewing ? 'Review' : 'Repository';
  $('tab-changes').setAttribute('aria-pressed', String(stops)); $('tab-files').setAttribute('aria-pressed', String(!stops));
  $('filter').placeholder = stops ? 'Filter changed units /' : 'Find file or symbol /';
  if (stops) { renderStops(); return; }
  $('count').textContent = `${state.files.length} files`;
  const query = $('filter').value.toLowerCase(); $('file-list').replaceChildren();
  for (const file of state.files) {
    if (file.path.toLowerCase().includes(query)) {
      const button = el('button', file.path, 'file-button' + (state.path === file.path ? ' active' : '')); button.title = file.path;
      button.addEventListener('click', () => openFile(file.path)); $('file-list').append(button);
    } else if (query) {
      for (const unit of file.units.filter(u => u.name.toLowerCase().includes(query))) {
        const button = el('button', `${unit.name} · ${file.path}`, 'file-button'); button.addEventListener('click', () => openFile(file.path, unit.id)); $('file-list').append(button);
      }
    }
  }
}
// The sidebar lists every change in the review, not only the stops a tour would cap.
function renderStops() {
  const review = state.review, query = $('filter').value.toLowerCase(), list = $('file-list'); list.replaceChildren();
  $('count').textContent = `${review.changes.filter(change => state.read.has(change.id)).length} / ${review.changes.length} read`;
  list.append(el('p', `${review.files} files · +${review.added} −${review.removed}${review.generated_files ? ` · ${review.generated_files} generated collapsed` : ''}`, 'review-facts'));
  for (const signal of review.signals) list.append(el('p', signal.detail, 'review-facts'));
  for (const group of window.tlcrReview.groups(review.changes)) {
    const rows = group.items.filter(({change}) => !query || `${change.node.name} ${change.node.path}`.toLowerCase().includes(query));
    if (!rows.length) continue;
    list.append(el('h3', group.cohort.replace(/^\d+ · /, ''), 'cohort'));
    for (const {index, change} of rows) {
      const read = state.read.has(change.id), button = el('button', undefined, 'file-button stop' + (index === state.stop && state.changeView ? ' active' : '') + (read ? ' read' : ''));
      button.title = `${change.status} · ${change.node.path}:${change.node.start}${read ? ' · read' : ''}`;
      const mark = el('span', read ? '✓' : '·', 'read-mark'); mark.setAttribute('aria-label', read ? 'read' : 'unread');
      button.append(mark, el('span', change.node.name, 'stop-name'), el('span', `+${change.added} −${change.removed}`, 'delta'));
      button.addEventListener('click', () => visitStop(index)); list.append(button);
    }
  }
}
function leaveStop() { if (state.tour?.id === 'changes' && state.changeView) state.paused = true; state.changeView = null; }
function showOverview() {
  state.sequence++; state.request?.abort(); window.tlcrHighlight.cancel(); state.path = null; state.unit = null; leaveStop(); state.orientation = null;
  $('filename').textContent = state.overview.name; $('filekind').textContent = 'Repository orientation · local snapshot'; $('unitlabel').textContent = 'Choose a starting point or follow a tour';
  $('units').replaceChildren(); $('stop-reason').hidden = true; $('detail').replaceChildren(); renderFiles();
  const welcome = el('div', undefined, 'welcome'); welcome.append(el('div', 'YOUR READING MAP', 'eyebrow'), el('h1', 'Understand how this system fits together.'), el('p', 'Start with an entry point. Follow its dependencies and tests. Every relationship leads back to source evidence.', 'muted'));
  const stats = el('div', undefined, 'stats');
  for (const [count, label] of [[state.overview.files,'files'],[state.overview.units,'source units'],[state.overview.relationships,'relationships']]) { const stat = el('div'); stat.append(el('strong', count), document.createTextNode(label)); stats.append(stat); }
  welcome.append(stats, el('h3', 'Where to start'));
  for (const node of state.overview.entry_points) welcome.append(destination(node, node.reasons.join(' · '), node.provenance));
  if (!state.overview.entry_points.length) welcome.append(el('p', 'No structural units found. Browse recognized files or choose a smaller source root.', 'muted'));
  $('code').replaceChildren(welcome);
  const guidance = card('A reading path, not a prompt'); guidance.append(el('p', 'Architecture follows declared entry points and known local calls. Testing pairs production units with tests that directly call them. Data & State shows source-backed boundary signals.'));
  const begin = el('button','Start Architecture Tour','accent'); begin.addEventListener('click', () => startTour('architecture')); guidance.append(begin);
  const limits = card('What the map knows'); limits.append(el('p','Ranking is a transparent heuristic. Unresolved calls, dynamic dispatch, external libraries and unavailable tools are not guessed.','muted'));
  for (const item of state.overview.diagnostics) limits.append(el('p',`${item.path ? item.path + ': ' : ''}${item.message}`,'warning'));
  const privacy = card('Local by default'); privacy.append(el('p','Browsing, relationships, evidence and tours work without a model or account. Optional AI interprets an exact payload you can inspect before approving.','muted'));
  $('detail').append(guidance, limits, privacy); updateTour();
}
async function navigate(node, history = true) {
  if (node.kind === 'package') { notice('Choose a source file in this package.'); return; }
  await openFile(node.path, node.unit_id || null, history);
}
async function openFile(path, unitID = null, history = true) {
  const sequence = ++state.sequence; state.request?.abort(); state.request = new AbortController(); const signal = state.request.signal;
  notice(); state.prepared = null; state.orientation = null; leaveStop(); updateTour();
  try {
    const data = await request('/api/file?path=' + encodeURIComponent(path), {signal}); if (sequence !== state.sequence) return;
    state.path = path; state.source = data.source; state.sourceLines = data.source.split('\n'); state.highlighted = null; state.codeWindow = 0; state.entry = data; state.unit = null;
    window.tlcrHighlight.tokenize(data.source, data.kind, path).then(lines => {
      if (state.path !== path || state.source !== data.source || !lines) return;
      state.highlighted = lines; const top = $('code').scrollTop, left = $('code').scrollLeft;
      renderCode(state.codeWindow, false); $('code').scrollTop = top; $('code').scrollLeft = left;
    });
    $('filename').textContent = path; $('filekind').textContent = `${data.kind} · ${data.units.length} readable units`; $('units').replaceChildren(); renderFiles();
    for (const unit of data.units) { const button = el('button', unit.name, 'unit'); button.title = `Lines ${unit.start}–${unit.end}`; button.dataset.id = unit.id; button.addEventListener('click', () => selectUnit(unit, true)); $('units').append(button); }
    const unit = data.units.find(u => u.id === unitID) || data.units[0];
    if (unit) await selectUnit(unit, history, sequence); else { renderCode(); $('detail').replaceChildren(el('p','No readable units.','muted')); }
  } catch (error) { if (error.name !== 'AbortError' && sequence === state.sequence) { notice(error.message); $('detail').replaceChildren(el('p',error.status === 409 ? 'Refresh to rebuild the local map before continuing.' : error.message,'warning')); } }
}
function renderCode(start = null, scroll = true) {
  const fragment = document.createDocumentFragment(), total = state.sourceLines.length, windowSize = 2000;
  start = start ?? Math.max(0, (state.unit?.start || 1) - 40);
  start = Math.min(start, Math.max(0, total - windowSize)); state.codeWindow = start;
  const end = Math.min(total, start + windowSize);
  function page(label, offset) { const button = el('button', label, 'source-page'); button.addEventListener('click', () => { renderCode(offset, false); $('code').scrollTop = 0; }); return button; }
  if (start > 0) fragment.append(page(`← Earlier lines · showing ${start+1}–${end} of ${total}`, Math.max(0, start-windowSize)));
  for (let i = start; i < end; i++) {
    const line = el('div', undefined, 'line'); line.dataset.line = i+1;
    if (state.unit && i+1 >= state.unit.start && i+1 <= state.unit.end) line.classList.add('selected');
    const content = el('span', undefined, 'line-content'), tokens = state.highlighted?.[i];
    if (tokens?.length) for (const token of tokens) content.append(el('span', token.text, /^[a-z-]+$/.test(token.type) ? `token ${token.type}` : ''));
    else content.textContent = state.sourceLines[i] || ' ';
    line.append(el('span', i+1, 'line-number'), content); fragment.append(line);
  }
  if (end < total) fragment.append(page(`Later lines → · showing ${start+1}–${end} of ${total}`, end));
  $('code').replaceChildren(fragment);
  const selected=$('code').querySelector('.line.selected'); if(scroll && selected) $('code').scrollTop=Math.max(0,selected.offsetTop-$('code').clientHeight/2);
}

async function selectUnit(unit, history = true, inheritedSequence = null) {
  const sequence = inheritedSequence ?? ++state.sequence; state.unit = unit; state.orientation = null; state.prepared = null;
  $('unitlabel').textContent = `${unit.kind} · lines ${unit.start}–${unit.end}`;
  for (const button of $('units').children) button.classList.toggle('active',button.dataset.id === unit.id);
  renderCode(); $('detail').replaceChildren(el('p','Reading local relationships…','muted'));
  if (history) recordHistory({path:state.path,unitID:unit.id}); updateStopReason();
  try {
    const data = await request('/api/node?id=' + encodeURIComponent('unit:' + unit.id)); if (sequence !== state.sequence) return;
    state.orientation = data; renderOrientation(data); loadEvidence(data.node.id, sequence);
  } catch (error) { if (sequence === state.sequence) { $('detail').replaceChildren(el('p',error.message,'warning')); notice('Refresh rebuilds the graph after source or exclusion changes.'); } }
}
function renderOrientation(data) { $('detail').replaceChildren(orientationCards(data)); document.querySelector('.context-body').scrollTop=0; }
function orientationCards(data) {
  const fragment = document.createDocumentFragment(); const intro = card('SOURCE FACTS'); intro.append(el('h3',data.node.name));
  const roles = el('div',undefined,'roles'); for (const role of data.node.roles) roles.append(el('span',role,'badge')); intro.append(roles);
  const callers = data.incoming.filter(e=>e.kind==='calls').length, dependencies = data.outgoing.filter(e=>e.kind==='calls'||e.kind==='local-module-source').length, tests = data.outgoing.filter(e=>e.kind==='tested-by').length;
  intro.append(el('p',`${callers} known callers · ${dependencies} local dependencies · ${tests} direct test relationships`)); provenance(intro,data.node.provenance);
  intro.append(el('p',`Heuristic importance ${data.node.score}: ${data.node.reasons.join(' · ')}`,'meta')); fragment.append(intro);
  const next = card('READ NEXT','read-next');
  for (const item of data.next) { const button = destination(item.node,item.reason,item.provenance); button.dataset.relationship = item.relationship; next.append(button); }
  if (!data.next.length) next.append(el('p','No resolved neighbors for this unit. Try a tour or another source unit.','muted')); fragment.append(next);
  const evidence = card('LOCAL EVIDENCE','evidence'); evidence.append(el('p','Loading local history and document references…','muted')); fragment.append(evidence);
  const ai = card('OPTIONAL INTERPRETATION','ai-section'); ai.classList.add('ai-action');
  ai.append(el('p','Enrich these facts with AI after reviewing the exact source and evidence to be sent.','muted'));
  const label = el('label'); const checkbox = el('input'); checkbox.type = 'checkbox'; checkbox.id = 'include-evidence'; checkbox.checked = true; label.append(checkbox,document.createTextNode(' Include bounded local evidence')); ai.append(label);
  const button = el('button','Preview AI enrichment'); button.id = 'preview-ai'; button.disabled = !state.provider || !state.model; button.addEventListener('click',previewAI); ai.append(button);
  if (button.disabled) ai.append(el('p','No model configured. The reading map above is fully available.','meta')); fragment.append(ai);
  return fragment;
}
async function loadEvidence(id,sequence) {
  try {
    const result = await request('/api/evidence?id='+encodeURIComponent(id)); if (sequence !== state.sequence) return;
    const box = $('evidence'); if (!box) return; box.replaceChildren(el('h3','LOCAL EVIDENCE'));
    for (const item of result.items) {
      const row = el('div',undefined,'evidence-item'); row.append(el('strong',item.label),el('p',item.detail)); provenance(row,item.provenance);
      if (item.node_id && item.provenance.path) { const button = el('button','Inspect provenance'); button.addEventListener('click',()=>{const file=state.files.find(f=>f.path===item.provenance.path);const unit=file?.units.find(u=>u.start<=item.provenance.start&&u.end>=item.provenance.start);openFile(item.provenance.path,unit?.id);}); row.append(button); } box.append(row);
    }
    if (!result.items.length) box.append(el('p','No matching local evidence found.','muted'));
    for (const d of result.diagnostics) box.append(el('p',d.message,'meta'));
  } catch (error) { if (sequence === state.sequence && $('evidence')) $('evidence').append(el('p',error.message,'warning')); }
}
function recordHistory(item) {
  const current = state.history[state.cursor]; if (current?.path === item.path && current?.unitID === item.unitID) return;
  state.history = state.history.slice(0,state.cursor+1); state.history.push(item); if (state.history.length > 200) state.history.shift(); state.cursor = state.history.length-1; updateHistory();
}
function updateHistory() { $('back').disabled = state.cursor <= 0; $('forward').disabled = state.cursor >= state.history.length-1; }
function historyMove(delta) { const cursor = state.cursor+delta; if (cursor<0 || cursor>=state.history.length) return; state.cursor=cursor; updateHistory(); const item=state.history[cursor]; openFile(item.path,item.unitID,false); }
async function startTour(kind = $('tour-select').value) {
  notice('Building a local reading path…');
  try {
    const tour = await request('/api/tour?kind='+encodeURIComponent(kind));
    state.review=null; state.selection=null; renderReviewBar();
    state.tour=tour; state.stop=0; state.paused=false; $('tour-select').value=kind;
    if (!tour.stops.length) { state.paused=true; notice(tour.limitations.join(' ')); updateTour(); return; }
    await visitStop(0); notice(tour.limitations.join(' '));
  } catch (error) { notice(error.message); }
}
async function visitStop(index) {
  if (!state.tour || index<0 || index>=state.tour.stops.length) return;
  state.stop=index; state.paused=false; updateTour();
  if (state.tour.id === 'changes') showChange(state.review.changes[index]);
  else await navigate(state.tour.stops[index].node);
  updateStopReason();
}
async function openReviewPicker() {
  $('review-error').textContent=''; $('review-refresh').hidden=true; $('review-filter').value=''; state.choice=0;
  $('review-list').replaceChildren(el('p','Reading local Git…','muted')); $('review-dialog').showModal(); $('review-filter').focus();
  try { state.changes = await request('/api/changes'); } catch (error) { state.changes = {working:[],commits:[],branches:[],default_branch:'',notes:[error.message]}; }
  renderChoices();
}
function renderChoices() {
  if (!state.changes) return;
  const list=$('review-list'); state.choices = window.tlcrReview.choices(state.changes, $('review-filter').value);
  state.choice = Math.max(0, Math.min(state.choice, state.choices.length-1)); list.replaceChildren(); let section='';
  state.choices.forEach((item, index) => {
    if (item.section !== section) { section=item.section; list.append(el('h3', section)); }
    const change=item.change, nothing=['uncommitted','staged','unstaged'].includes(change.kind) && !change.files;
    const button=el('button', undefined, 'choice'+(index===state.choice?' active':'')+(nothing?' nothing':'')); button.setAttribute('role','option'); button.setAttribute('aria-selected', String(index===state.choice));
    button.append(el('strong', change.title), el('small', [change.id, change.date, change.detail, nothing ? 'nothing to review' : ''].filter(Boolean).join(' · ')));
    button.addEventListener('click', () => chooseReview(change.selection)); list.append(button);
  });
  if (!state.choices.length) list.append(el('p','Nothing matches. Type a revision such as HEAD~2, or a range such as main..feature.','muted'));
  for (const note of state.changes.notes) list.append(el('p', note, 'meta'));
  list.querySelector('.active')?.scrollIntoView({block:'nearest'});
}
async function chooseReview(selection, refreshFirst = false) {
  $('review-error').textContent = refreshFirst ? 'Refreshing and comparing…' : 'Comparing local snapshots…'; $('review-refresh').hidden=true;
  try {
    if (refreshFirst) { await post('/api/refresh',{}); await loadRepository(); }
    await startReview(selection); $('review-dialog').close(); $('code').focus();
  } catch (error) { $('review-error').textContent=error.message; state.pending=selection; $('review-refresh').hidden = error.status !== 409; }
}
// Starts a review of the selection, optionally returning to the stop for a known unit.
async function startReview(selection, keepID = null) {
  const review = await request('/api/review?' + window.tlcrReview.query(selection));
  if (!review.changes.length) throw new Error(`No indexed source differs between ${review.base_label} and ${review.head_label}.`);
  const detail = `${review.base_label} → ${review.head_label}`;
  state.review=review; state.selection=selection; state.sidebar='changes'; $('filter').value='';
  state.tour={id:'changes', title:'Change Tour', limitations:review.limitations, stops:review.changes.map(change => ({node:change.node, reason:`${change.status} · ${change.cohort.replace(/^\d+ · /,'')}`, provenance:{provider:'Local Git comparison', path:change.node.path, start:change.node.start, end:change.node.end, detail}}))};
  renderReviewBar(); notice();
  await visitStop(Math.max(0, keepID ? review.changes.findIndex(change => change.node.id === keepID) : 0));
}
function endReview() { state.review=null; state.selection=null; state.tour=null; state.paused=false; renderReviewBar(); showOverview(); }
function renderReviewBar() {
  const review=state.review; $('review-summary').hidden=!review; $('review-end').hidden=!review; $('review-open').textContent = review ? 'Change…' : 'Review a change';
  if (review) { $('review-summary').textContent=`Reviewing ${review.base_label} → ${review.head_label} · ${review.files} files · +${review.added} −${review.removed}`; $('review-summary').title=review.limitations.join(' '); }
}
function currentChange() { return state.review && state.changeView ? state.review.changes[state.stop] : null; }
function unitExists(node) { return state.files.some(file => file.path === node.path && (!node.unit_id || file.units.some(unit => unit.id === node.unit_id))); }
// Opens current source for a node taken from a reviewed snapshot, and says so when the two differ.
async function openReviewed(node) {
  try {
    const current = await request('/api/node?id=' + encodeURIComponent(node.id)); await navigate(current.node);
    if (current.node.source_hash !== node.source_hash) notice('Current source of this file differs from the reviewed snapshot.');
  } catch (error) { notice(error.status === 404 ? `${node.name} is not in the current working tree.` : error.message); }
}
function reviewedDestination(item) {
  const direct = state.review.live && unitExists(item.node), button = destination(item.node, item.reason, item.provenance, direct ? navigate : openReviewed);
  button.dataset.relationship = item.relationship; return button;
}
function showChange(change) {
  const sequence = ++state.sequence; state.request?.abort(); window.tlcrHighlight.cancel();
  state.prepared = null; state.orientation = null; state.path = change.node.path; state.changeView = change.node.id;
  const file = state.review.live && change.status !== 'removed' ? state.files.find(item => item.path === change.node.path) : null;
  state.unit = file?.units.find(unit => unit.id === change.node.unit_id) || null;
  $('filename').textContent = change.node.path; $('units').replaceChildren(); $('unitlabel').textContent = change.node.name;
  $('filekind').textContent = `${change.status}${change.whitespace_only ? ' · whitespace only' : ''} · +${change.added} −${change.removed} · ${state.diffMode ? 'diff' : 'full source'} (v)`;
  renderFiles(); renderChangeSource(change); $('code').scrollTop = 0; renderChangeContext(change, sequence);
  if (state.diffMode && change.hunks.length) highlightChange(change, sequence);
}
function renderChangeSource(change, tokens = null) {
  const view = el('div', undefined, 'change-source');
  if (state.diffMode && change.hunks.length) {
    for (const row of window.tlcrReview.rows(change, tokens?.before, tokens?.after)) {
      if (row.hunk) { view.append(el('div', row.hunk, 'hunk')); continue; }
      const line = el('div', undefined, 'line diff-line ' + ({'+':'added','-':'removed'}[row.op] || 'context')), content = el('span', undefined, 'line-content');
      if (row.tokens?.length) for (const token of row.tokens) content.append(el('span', token.text, /^[a-z-]+$/.test(token.type) ? `token ${token.type}` : ''));
      else content.textContent = row.text || ' ';
      const marker = el('span', row.op === '-' ? '−' : row.op, 'diff-marker'); if (row.op !== ' ') marker.setAttribute('aria-label', row.op === '+' ? 'added line' : 'removed line');
      line.append(el('span', row.before || '', 'line-number'), el('span', row.after || '', 'line-number'), marker, content); view.append(line);
    }
  } else {
    view.classList.add('full');
    if (!change.hunks.length) view.append(el('p', 'No line differences: this stop is listed because its relationships changed.', 'muted'));
    for (const [title, source, start] of [[`BASE · ${state.review.base_label}`, change.before_source, change.before?.start || 1], [`HEAD · ${state.review.head_label}`, change.after_source, change.node.start]]) {
      const part = el('section'); part.append(el('h3',`${title} · ${(change.before && title.startsWith('BASE') ? change.before : change.node).path}:${start}`),el('pre', source || '(No source on this side)', 'change-code')); view.append(part);
    }
  }
  $('code').replaceChildren(view);
}
// Tokenizes each side as a whole so multi-line tokens are right, then redraws in place.
async function highlightChange(change, sequence) {
  const kind = change.node.language, path = change.node.path, shown = () => sequence === state.sequence && state.diffMode;
  const before = change.before_source ? await window.tlcrHighlight.tokenize(change.before_source, kind, path) : null; if (!shown()) return;
  const after = change.after_source ? await window.tlcrHighlight.tokenize(change.after_source, kind, path) : null; if (!shown() || !(before || after)) return;
  const top = $('code').scrollTop, left = $('code').scrollLeft; renderChangeSource(change, {before, after}); $('code').scrollTop = top; $('code').scrollLeft = left;
}
function renderChangeContext(change, sequence) {
  const review = state.review, facts = card('CHANGE'); facts.append(el('h3',change.node.name),el('p',`${change.status} · ${change.cohort.replace(/^\d+ · /,'')} · +${change.added} −${change.removed}${change.whitespace_only ? ' · whitespace only' : ''}`));
  if (change.before && (change.status === 'renamed' || change.status === 'moved')) facts.append(el('p',`Was ${change.before.name} · ${change.before.path}:${change.before.start}`,'meta'));
  facts.append(el('p',`${review.base_label} → ${review.head_label}`,'meta'));
  const mark = el('button', state.read.has(change.id) ? 'Mark unread (x)' : 'Mark read (x)'); mark.id = 'mark-read'; mark.addEventListener('click', toggleRead); facts.append(mark);
  const signals = card('AROUND THIS CHANGE','signals');
  for (const signal of change.signals) { signals.append(el('p',signal.detail)); for (const item of signal.related) signals.append(reviewedDestination(item)); }
  if (!change.signals.length) signals.append(el('p','No caller or test signals for this stop.','muted'));
  const relationships = card('RELATIONSHIP CHANGES');
  for (const [label, edges] of [['Added', change.relationships_added],['Removed',change.relationships_removed]]) for (const edge of edges) { relationships.append(el('p',`${label}: ${edge.kind} → ${edge.to}`)); provenance(relationships,edge.provenance); }
  if (!change.relationships_added.length && !change.relationships_removed.length) relationships.append(el('p','No resolved relationship changes for this stop.','muted'));
  const rest = el('div'); $('detail').replaceChildren(facts,signals,relationships,rest); document.querySelector('.context-body').scrollTop=0;
  if (state.unit) {
    // The head is the working tree and the unit exists: the whole reading toolkit applies.
    rest.append(el('p','Reading local relationships…','muted'));
    request('/api/node?id=' + encodeURIComponent(change.node.id)).then(data => { if (sequence !== state.sequence) return; state.orientation = data; rest.replaceChildren(orientationCards(data)); loadEvidence(data.node.id, sequence); })
      .catch(error => { if (sequence === state.sequence) rest.replaceChildren(el('p',error.message,'warning')); });
    return;
  }
  const related = card('NEIGHBORS IN THE REVIEWED SNAPSHOT','read-next');
  related.append(el('p', review.live ? 'This unit is not in the working tree, so relationships come from the base.' : 'The head is not the working tree, so relationships come from the reviewed snapshot. Links open current source and say when it differs.', 'meta'));
  for (const item of change.related) related.append(reviewedDestination(item));
  if (!change.related.length) related.append(el('p','No resolved neighbors for this stop.','muted'));
  rest.append(related);
}
function toggleRead() {
  const change = currentChange(); if (!change) { notice('Open a change stop to mark it.'); return; }
  if (!state.read.delete(change.id)) state.read.add(change.id);
  const button = $('mark-read'); if (button) button.textContent = state.read.has(change.id) ? 'Mark unread (x)' : 'Mark read (x)';
  renderFiles(); updateTour();
}
function nextUnread() {
  if (!state.review) { notice('Start a review to track unread stops.'); return; }
  const index = window.tlcrReview.nextUnread(state.review.changes, state.read, state.stop);
  if (index < 0) notice('Every stop in this review is marked read.'); else visitStop(index);
}
function toggleDiff() { const change = currentChange(); if (!change) return; state.diffMode = !state.diffMode; showChange(change); }
function moveHunk(delta) {
  const code = $('code'), hunks = [...code.querySelectorAll('.hunk')], top = code.scrollTop;
  const target = delta > 0 ? hunks.find(hunk => hunk.offsetTop > top + 4) : hunks.reverse().find(hunk => hunk.offsetTop < top - 4);
  if (target) code.scrollTop = target.offsetTop; else if (hunks.length) notice(delta > 0 ? 'Last hunk of this stop.' : 'First hunk of this stop.');
}

function updateTour() {
  const active=!!state.tour?.stops.length && !state.paused;
  $('tour-progress').hidden=!active; $('resume-tour').hidden=!state.tour?.stops.length || !state.paused;
  $('resume-tour').textContent = state.review ? `Back to stop ${state.stop+1} (b)` : 'Resume tour';
  $('tour-position').textContent=state.tour ? `${state.stop+1} / ${state.tour.stops.length}${state.review ? ` · ${state.review.changes.filter(change => state.read.has(change.id)).length} read` : ''}` : '';
  $('tour-prev').disabled=!active || state.stop===0; $('tour-next').disabled=!active || state.stop===state.tour.stops.length-1; updateStopReason();
}
function updateStopReason() {
  const stop=state.tour?.stops[state.stop]; const visible=stop && !state.paused && (state.changeView === stop.node.id || (stop.node.unit_id===state.unit?.id && stop.node.path===state.path));
  $('stop-reason').hidden=!visible; if (visible) { $('stop-reason').replaceChildren(el('strong',`${state.tour.title} · ${state.stop+1}/${state.tour.stops.length} — ${stop.reason}`)); provenance($('stop-reason'),stop.provenance); }
}
async function refresh() {
  const previous=state.path, unit=state.unit?.id; notice('Refreshing local analysis…'); state.request?.abort(); state.sequence++;
  try { await post('/api/refresh',{}); await loadRepository();
    if (state.review) {
      // Keep the selection: rebuild the review and return to the same unit when it is still part of it.
      const keep = state.review.changes[state.stop]?.node.id;
      try { await startReview(state.selection, keep); notice('Local map refreshed; review rebuilt.'); } catch (error) { endReview(); notice(error.message); }
      return;
    }
    state.tour=null; state.paused=false; updateTour(); if (previous && state.files.some(f=>f.path===previous)) await openFile(previous,unit,false); else showOverview(); notice('Local map refreshed.'); } catch(error) {notice(error.message);}
}
async function previewAI() {
  if (!state.unit || !state.provider || !state.model) return;
  const sequence=state.sequence, path=state.path, unitID=state.unit.id, enrich=$('include-evidence')?.checked ?? true;
  const button=$('preview-ai'); if(button)button.disabled=true;
  try { const prepared=await post('/api/explain/preview',{path,unit_id:unitID,enrich}); if(sequence!==state.sequence)return;
    state.prepared={...prepared,path,unit_id:unitID,enrich,sequence}; $('ai-destination').textContent=`${prepared.provider} · ${prepared.model} · approximately ${prepared.estimated_tokens} input tokens`;
    $('ai-prompt').value=prepared.prompt; $('ai-error').textContent=''; $('send-ai').disabled=false; $('ai-dialog').showModal();
  } catch(error) {notice(error.message);} finally {if(button)button.disabled=false;}
}
async function sendAI() {
  const prepared=state.prepared; if(!prepared)return; $('send-ai').disabled=true; $('ai-error').textContent='Requesting interpretation…';
  try { const result=await post('/api/explain',{path:prepared.path,unit_id:prepared.unit_id,enrich:prepared.enrich,digest:prepared.digest,approved:true}); $('ai-dialog').close();
    if (prepared.sequence===state.sequence && $('ai-section')) { const box=$('ai-section'); box.replaceChildren(el('h3','AI INTERPRETATION'),renderMarkdown(result.text),el('p',result.cached?'Cached interpretation · no model call':`${result.input_tokens} input · ${result.output_tokens} output tokens`,'meta')); }
    const tree=await request('/api/tree'); $('budget').textContent=`${tree.used_input_tokens} / ${tree.session_input_budget} input tokens`;
  } catch(error) { $('ai-error').textContent=error.message; } finally {$('send-ai').disabled=false;}
}
const bindings = {'next':'j / n','previous':'k / p','back':'[','forward':']','search':'/ or g f','tour':'t','read-next':'r','tests':'g t','callers':'g c','dependencies':'g d','evidence':'w','explain':'e','review':'c','mark-read':'x','next-unread':'u','next-hunk':'J','previous-hunk':'K','toggle-diff':'v','resume':'b','help':'?','escape':'Esc'};
function renderHelp() { $('shortcut-list').replaceChildren(); for(const cmd of state.commands){const row=el('div',undefined,'shortcut');row.append(el('span',cmd.label),el('kbd',bindings[cmd.id]||'Menu'));$('shortcut-list').append(row);} }
function focusRelationship(kinds) { const target=[...document.querySelectorAll('#detail button[data-relationship]')].find(b=>kinds.includes(b.dataset.relationship)); if(target)target.focus();else notice('No resolved relationship of this kind at this stop.'); }
const actions={
 next:()=>state.tour&&!state.paused?visitStop(state.stop+1):state.orientation?.next[0]&&navigate(state.orientation.next[0].node),previous:()=>state.tour&&!state.paused?visitStop(state.stop-1):historyMove(-1),back:()=>historyMove(-1),forward:()=>historyMove(1),search:()=>$('filter').focus(),tour:()=>$('tour-select').focus(),
 'read-next':()=>document.querySelector('#read-next button')?.focus(),tests:()=>focusRelationship(['tested-by','tests']),callers:()=>focusRelationship(['called-by']),dependencies:()=>focusRelationship(['calls','local-module-source','imports']),
 evidence:()=>$('evidence')?.scrollIntoView({block:'start'}),explain:previewAI,review:openReviewPicker,'mark-read':toggleRead,'next-unread':nextUnread,'next-hunk':()=>moveHunk(1),'previous-hunk':()=>moveHunk(-1),'toggle-diff':toggleDiff,resume:()=>state.tour&&visitStop(state.stop),help:()=>$('help-dialog').showModal(),escape:()=>{for(const d of document.querySelectorAll('dialog[open]'))d.close();}
};
let chord=false, chordTimer;
document.addEventListener('keydown',event=>{
 if(event.metaKey||event.ctrlKey||event.altKey||event.isComposing)return;
 if(event.key==='Escape'){chord=false;return;}
 if(event.target.closest?.('input,textarea,select,[contenteditable="true"]')||document.querySelector('dialog[open]'))return;
 const key=event.key;let command;
 if(chord){chord=false;clearTimeout(chordTimer);command={f:'search',t:'tests',c:'callers',d:'dependencies'}[key];}
 else if(key==='g'){chord=true;chordTimer=setTimeout(()=>{chord=false;},1000);event.preventDefault();return;}
 else command={j:'next',n:'next',k:'previous',p:'previous','[':'back',']':'forward','/':'search',t:'tour',r:'read-next',w:'evidence',e:'explain',c:'review',x:'mark-read',u:'next-unread',J:'next-hunk',K:'previous-hunk',v:'toggle-diff',b:'resume','?':'help'}[key];
 if(command){event.preventDefault();actions[command]();}
});
$('review-open').addEventListener('click',openReviewPicker);$('review-end').addEventListener('click',endReview);$('close-review').addEventListener('click',()=>$('review-dialog').close());
$('review-refresh').addEventListener('click',()=>chooseReview(state.pending,true));
$('review-filter').addEventListener('input',()=>{state.choice=0;renderChoices();});
$('review-filter').addEventListener('keydown',event=>{
 if(event.isComposing)return;
 const step={ArrowDown:1,ArrowUp:-1}[event.key];
 if(step){event.preventDefault();state.choice=Math.max(0,Math.min(state.choices.length-1,state.choice+step));renderChoices();}
 else if(event.key==='Enter'){event.preventDefault();const item=state.choices[state.choice];if(item)chooseReview(item.change.selection);}
});
$('tab-changes').addEventListener('click',()=>{state.sidebar='changes';renderFiles();});$('tab-files').addEventListener('click',()=>{state.sidebar='files';renderFiles();});
$('filter').addEventListener('input',renderFiles); $('refresh').addEventListener('click',refresh); $('home').addEventListener('click',showOverview);
$('back').addEventListener('click',()=>historyMove(-1));$('forward').addEventListener('click',()=>historyMove(1));$('start-tour').addEventListener('click',()=>startTour());
$('tour-prev').addEventListener('click',()=>visitStop(state.stop-1));$('tour-next').addEventListener('click',()=>visitStop(state.stop+1));$('pause-tour').addEventListener('click',()=>{state.paused=true;updateTour();});$('resume-tour').addEventListener('click',()=>visitStop(state.stop));
$('help').addEventListener('click',actions.help);$('close-help').addEventListener('click',()=>$('help-dialog').close());
$('close-ai').addEventListener('click',()=>$('ai-dialog').close());$('cancel-ai').addEventListener('click',()=>$('ai-dialog').close());$('send-ai').addEventListener('click',sendAI);
loadRepository().then(showOverview).catch(error=>notice(error.message));
