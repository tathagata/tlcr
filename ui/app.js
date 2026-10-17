'use strict';
const $ = id => document.getElementById(id);
const state = { files: [], overview: null, commands: [], path: null, unit: null, source: '', sourceLines: [], highlighted: null, codeWindow: 0, entry: null, orientation: null, sequence: 0, history: [], cursor: -1, tour: null, review: null, selection: null, changes: null, choices: [], choice: 0, pending: null, repository: null, notes: {}, read: new Set(), sidebar: 'stops', open: new Set(['']), diffMode: true, changeView: null, stop: 0, paused: false, visiting: false, finished: false, provider: '', destination: '', model: '', request: null };
const el = (tag, text, className) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; if (className) n.className = className; return n; };
function notice(text = '') { $('notice').textContent = text; }
async function request(url, options = {}) {
  const response = await fetch(url, options);
  const data = await response.json();
  if (!response.ok) { const error = new Error(data.error || `HTTP ${response.status}`); error.status = response.status; throw error; }
  return data;
}
const post = (url, body, signal) => request(url, {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body), signal});
function sourceLabel(p) { return `${p.path}${p.start ? ':' + p.start + (p.end > p.start ? '–' + p.end : '') : ''}`; }
function provenance(parent, p) { parent.append(el('small', `${p.provider} · ${sourceLabel(p)}${p.detail ? ' · ' + p.detail : ''}`, 'meta')); }
function destination(node, reason, p, open = navigate) {
  const button = el('button', undefined, 'destination'); button.append(el('strong', window.tlcrWalk.short(node.name)), el('small', `${node.path}:${node.start || 1} · ${reason}`));
  if (p) button.title = `${p.provider}: ${p.detail} (${sourceLabel(p)})`;
  button.addEventListener('click', () => open(node)); return button;
}
function storage() { try { return window.localStorage; } catch { return null; } }
function persist() { if (!window.tlcrReview.save(storage(), state.repository, state.read, state.notes)) notice('This browser is not keeping review marks and notes; they last until the page closes.'); }
function card(title, id) { const box = el('div', undefined, 'card'); if (id) box.id = id; box.append(el('h3', title)); return box; }
// Renders narration paragraphs; a named unit with a node becomes a link that leaves the tour.
function renderStory(paragraphs, open = navigate) {
  const box = $('walk-story'); box.replaceChildren();
  for (const parts of paragraphs) {
    const p = el('p');
    for (const part of parts) {
      if (typeof part === 'string') p.append(part);
      else if (part.node) { const link = el('button', part.code, 'code-link'); link.title = `${part.node.path}:${part.node.start || 1}`; link.addEventListener('click', () => open(part.node)); p.append(link); }
      else p.append(el('code', part.code));
    }
    box.append(p);
  }
}
function walkHead(step, title) { $('walk-step').textContent = step; $('walk-title').textContent = title; $('walk-body').scrollTop = 0; }
function clearWalk() { for (const id of ['walk-story', 'walk-extra', 'walk-ai', 'walk-upnext', 'detail']) $(id).replaceChildren(); }
async function loadRepository() {
  const [tree, overview, commands] = await Promise.all([request('/api/tree'), request('/api/overview'), request('/api/commands')]);
  if (tree.repository !== state.repository) { state.repository = tree.repository; const kept = window.tlcrReview.load(storage(), tree.repository); state.read = kept.read; state.notes = kept.notes; }
  state.files = tree.files; state.overview = overview; state.commands = commands; state.provider = tree.provider; state.destination = tree.destination; state.model = tree.model;
  for (const node of overview.entry_points) state.open.add(node.path.includes('/') ? node.path.slice(0, node.path.lastIndexOf('/')) : '');
  $('provider').textContent = tree.provider && tree.model ? `Local analysis · AI available: ${aiName()}` : 'Local analysis';
  ai.used = tree.used_input_tokens; ai.budget = tree.session_input_budget; renderAIBar();
  renderFiles(); renderHelp();
}
function renderFiles() {
  const touring = !!state.tour?.stops.length, stops = touring && state.sidebar === 'stops';
  $('side-tabs').hidden = !touring; $('side-title').textContent = state.review ? 'Review' : touring ? state.tour.title : 'Repository';
  $('tab-stops').textContent = state.review ? 'Changes' : 'Tour';
  $('tab-stops').setAttribute('aria-pressed', String(stops)); $('tab-files').setAttribute('aria-pressed', String(!stops));
  $('filter').placeholder = stops ? 'Filter stops /' : 'Find file or symbol /';
  if (stops) { renderStops(); return; }
  $('count').textContent = `${state.files.length} files`;
  const query = $('filter').value.toLowerCase(), list = $('file-list'); list.replaceChildren();
  for (const folder of window.tlcrWalk.folders(state.files, state.overview?.entry_points.map(node => node.path))) {
    const rows = [];
    for (const file of folder.files) {
      const name = file.path.slice(folder.dir ? folder.dir.length + 1 : 0);
      if (!query || file.path.toLowerCase().includes(query)) {
        const button = el('button', name, 'file-button' + (state.path === file.path ? ' active' : '')); button.title = file.path;
        button.addEventListener('click', () => openFile(file.path)); rows.push(button);
      } else for (const unit of file.units.filter(u => u.name.toLowerCase().includes(query))) {
        const button = el('button', `${window.tlcrWalk.short(unit.name)} · ${name}`, 'file-button'); button.title = `${unit.name} · ${file.path}`; button.addEventListener('click', () => openFile(file.path, unit.id)); rows.push(button);
      }
    }
    if (!rows.length) continue;
    const open = !!query || state.open.has(folder.dir), head = el('button', undefined, 'folder'); head.setAttribute('aria-expanded', String(open));
    head.append(el('span', open ? '▾' : '▸', 'twisty'), el('span', folder.label, 'folder-name'), el('small', folder.detail));
    head.addEventListener('click', () => { if (!state.open.delete(folder.dir)) state.open.add(folder.dir); renderFiles(); });
    list.append(head); if (open) list.append(...rows);
  }
}
// The sidebar outlines the tour as chapters. A review lists every change, not only the stops a tour would cap.
function renderStops() {
  const review = state.review, tour = state.tour, query = $('filter').value.toLowerCase(), list = $('file-list'); list.replaceChildren();
  const items = review ? review.changes.map(change => ({node: change.node, cohort: change.cohort, reason: change.status, change})) : tour.stops;
  $('count').textContent = review ? `${review.changes.filter(change => state.read.has(change.id)).length} / ${review.changes.length} read` : `${tour.stops.length} stops`;
  if (review) {
    list.append(el('p', `${review.files} files · +${review.added} −${review.removed}${review.generated_files ? ` · ${review.generated_files} generated collapsed` : ''}`, 'review-facts'));
  }
  for (const chapter of window.tlcrWalk.chapters(items, tour.id)) {
    const rows = chapter.items.filter(({stop}) => !query || `${stop.node.name} ${stop.node.path}`.toLowerCase().includes(query));
    if (!rows.length) continue;
    list.append(el('h3', chapter.title, 'cohort'));
    for (const {index, stop} of rows) {
      const change = stop.change, read = change && state.read.has(change.id), here = index === state.stop && !state.paused && !state.finished;
      const button = el('button', undefined, 'file-button stop' + (here ? ' active' : '') + (read ? ' read' : ''));
      button.title = `${stop.node.path}:${stop.node.start}${change ? ` · ${change.status}${read ? ' · read' : ''}` : ''}`;
      const mark = el('span', change ? (read ? '✓' : '·') : String(index + 1), 'read-mark'); if (change) mark.setAttribute('aria-label', read ? 'read' : 'unread');
      const name = el('span', undefined, 'stop-name'); name.append(el('span', window.tlcrWalk.short(stop.node.name)), el('small', stop.node.path));
      button.append(mark, name);
      if (change) button.append(el('span', `${state.notes[change.node.id] ? '✎ ' : ''}+${change.added} −${change.removed}`, 'delta'));
      button.addEventListener('click', () => visitStop(index)); list.append(button);
    }
  }
  if (!review) return;
  const tools = el('div', undefined, 'review-tools'), copy = el('button', 'Copy summary'), forget = el('button', 'Clear marks and notes');
  copy.addEventListener('click', copySummary);
  // Two clicks instead of a blocking confirmation dialog.
  forget.addEventListener('click', () => { if (forget.dataset.armed) { state.read = new Set(); state.notes = {}; window.tlcrReview.clear(storage(), state.repository); renderFiles(); updateTour(); const change = currentChange(); if (change) showChange(change); } else { forget.dataset.armed = 'yes'; forget.textContent = 'Click again to clear'; } });
  tools.append(copy, forget, el('p', 'Marks and notes stay in this browser, for this repository only.', 'review-facts')); list.append(tools);
}
async function copySummary() {
  const text = window.tlcrReview.summary(state.review, state.read, state.notes);
  try { await navigator.clipboard.writeText(text); notice('Review summary copied as Markdown.'); } catch { notice('The browser refused clipboard access; the summary was not copied.'); }
}
// Reading anything other than the current stop pauses the tour; Back to the tour returns to it.
function leaveStop() { if (state.tour && !state.visiting) state.paused = true; state.changeView = null; }
function tourButtons(parent) {
  const begin = el('button', 'Start the Architecture Tour', 'accent'); begin.addEventListener('click', () => startTour('architecture'));
  const review = el('button', 'Review a change (c)'); review.addEventListener('click', openReviewPicker);
  const row = el('div', undefined, 'walk-actions'); row.append(begin, review); parent.append(row);
}
function showOverview() {
  state.sequence++; state.request?.abort(); window.tlcrHighlight.cancel(); state.path = null; state.unit = null; leaveStop(); state.orientation = null; state.finished = false;
  $('filename').textContent = state.overview.name; $('filekind').textContent = 'Repository orientation · local snapshot';
  $('units').replaceChildren(); renderFiles();
  const welcome = el('div', undefined, 'welcome'); welcome.append(el('div', 'YOUR READING MAP', 'eyebrow'), el('h1', 'Understand how this system fits together.'), el('p', 'Start with an entry point. Follow its dependencies and tests. Every relationship leads back to source evidence.', 'muted'));
  const stats = el('div', undefined, 'stats');
  for (const [count, label] of [[state.overview.files,'files'],[state.overview.units,'source units'],[state.overview.relationships,'relationships']]) { const stat = el('div'); stat.append(el('strong', count), document.createTextNode(label)); stats.append(stat); }
  welcome.append(stats, el('h3', 'Where to start'));
  for (const node of state.overview.entry_points) welcome.append(destination(node, node.reasons.join(' · '), node.provenance));
  if (!state.overview.entry_points.length) welcome.append(el('p', 'No structural units found. Browse recognized files or choose a smaller source root.', 'muted'));
  $('code').replaceChildren(welcome);
  clearWalk(); walkHead('Welcome', 'Be walked through the code');
  renderStory([
    ['A tour takes you through this repository one stop at a time, the way its author might: the code on the left, and here, what it is, why you are looking at it and where it leads.'],
    ['The Architecture Tour starts where the program starts and follows what it calls. Pick another tour above, or review a change to walk through what it did.'],
  ]);
  tourButtons($('walk-extra'));
  const limits = card('What the map knows'); limits.append(el('p','Ranking is a transparent heuristic. Unresolved calls, dynamic dispatch, external libraries and unavailable tools are not guessed.','muted'));
  for (const item of state.overview.diagnostics) limits.append(el('p',`${item.path ? item.path + ': ' : ''}${item.message}`,'warning'));
  const privacy = card('Local by default'); privacy.append(el('p','Browsing, relationships, evidence and tours work without a model or account. Optional AI interprets an exact payload you can inspect before approving.','muted'));
  $('detail').append(limits, privacy); updateTour();
}
async function navigate(node, history = true) {
  if (node.kind === 'package') { notice('Choose a source file in this package.'); return; }
  await openFile(node.path, node.unit_id || null, history);
}
async function openFile(path, unitID = null, history = true) {
  const sequence = ++state.sequence; state.request?.abort(); state.request = new AbortController(); const signal = state.request.signal;
  notice(); state.orientation = null; state.finished = false; leaveStop(); updateTour();
  try {
    const data = await request('/api/file?path=' + encodeURIComponent(path), {signal}); if (sequence !== state.sequence) return;
    state.path = path; state.open.add(path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''); state.source = data.source; state.sourceLines = data.source.split('\n'); state.highlighted = null; state.codeWindow = 0; state.entry = data; state.unit = null;
    window.tlcrHighlight.tokenize(data.source, data.kind, path).then(lines => {
      if (state.path !== path || state.source !== data.source || !lines) return;
      state.highlighted = lines; const top = $('code').scrollTop, left = $('code').scrollLeft;
      renderCode(state.codeWindow, false); $('code').scrollTop = top; $('code').scrollLeft = left;
    });
    $('filename').textContent = path; $('filekind').textContent = `${data.kind} · ${data.units.length} readable units`; $('units').replaceChildren(); renderFiles();
    if (data.units.length > 1) for (const unit of data.units) { const button = el('button', window.tlcrWalk.short(unit.name), 'unit'); button.title = `${unit.name} · lines ${unit.start}–${unit.end}`; button.dataset.id = unit.id; button.addEventListener('click', () => { leaveStop(); selectUnit(unit, true); }); $('units').append(button); }
    const unit = data.units.find(u => u.id === unitID) || data.units[0];
    if (unit) await selectUnit(unit, history, sequence); else { renderCode(); clearWalk(); walkHead(path, 'No readable units'); }
  } catch (error) { if (error.name !== 'AbortError' && sequence === state.sequence) { notice(error.message); renderStory([[error.status === 409 ? 'Refresh to rebuild the local map before continuing.' : error.message]]); } }
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
  const selected=$('code').querySelector('.line.selected'); if(scroll && selected) $('code').scrollTop=Math.max(0,selected.offsetTop-$('code').clientHeight*0.2);
}
function onStop() { const stop = state.tour?.stops[state.stop]; return !!stop && !state.paused && !state.finished && stop.node.unit_id === state.unit?.id && stop.node.path === state.path; }
async function selectUnit(unit, history = true, inheritedSequence = null) {
  const sequence = inheritedSequence ?? ++state.sequence; state.unit = unit; state.orientation = null;
  $('filekind').textContent = `${state.entry?.kind || ''} · ${unit.kind} · lines ${unit.start}–${unit.end}`;
  for (const button of $('units').children) button.classList.toggle('active',button.dataset.id === unit.id);
  renderCode(); clearWalk(); renderWalkUnit(null, null);
  if (history) recordHistory({path:state.path,unitID:unit.id});
  const id = encodeURIComponent('unit:' + unit.id);
  try {
    // The brief reads Git, so the relationships render first and the brief joins them when it arrives.
    const brief = request('/api/brief?id=' + id).catch(() => null);
    const data = await request('/api/node?id=' + id); if (sequence !== state.sequence) return;
    state.orientation = data; renderWalkUnit(data, null); $('detail').replaceChildren(orientationCards(data)); loadEvidence(data.node.id, sequence);
    const facts = await brief; if (sequence !== state.sequence) return;
    if (facts) renderWalkUnit(data, facts);
    explainWithAI();
  } catch (error) { if (sequence === state.sequence) { renderStory([[error.message]]); notice('Refresh rebuilds the graph after source or exclusion changes.'); } }
}
// The walkthrough for a unit: the tour's narration at a stop, otherwise what the unit is and where to go from it.
function renderWalkUnit(around, brief) {
  const walk = window.tlcrWalk, unit = state.unit, top = $('walk-body').scrollTop;
  if (onStop()) {
    const chapter = walk.chapters(state.tour.stops, state.tour.id).find(item => item.items.some(({index}) => index === state.stop));
    walkHead(`${state.tour.title} · stop ${state.stop + 1} of ${state.tour.stops.length}${chapter ? ' · ' + chapter.title : ''}`, walk.short(unit.name));
    renderStory(walk.narrate(state.tour, state.stop, around, brief));
    const next = walk.upNext(state.tour, state.stop), box = $('walk-upnext'); box.replaceChildren();
    if (next) { const label = el('p', undefined, 'up-next'); label.append(el('span', 'Up next  '), el('strong', next.name), el('small', ' · ' + next.path)); box.append(label); }
  } else {
    walkHead(state.tour && state.paused ? `Off the ${state.tour.title}` : state.path, walk.short(unit.name));
    const node = around?.node || {name: unit.name, path: state.path, roles: []};
    renderStory([[{code: walk.short(node.name)}, ` is a ${unit.kind} in ${state.path}, lines ${unit.start}–${unit.end}.`], ...walk.describe(around, brief)]);
    if (around?.next.length) {
      const box = $('walk-upnext'); box.replaceChildren(el('h3', 'Where to go from here', 'walk-label'));
      for (const item of around.next.slice(0, 5)) { const button = destination(item.node, item.reason, item.provenance); button.dataset.relationship = item.relationship; box.append(button); }
    }
  }
  // A re-render when the brief arrives keeps the reader's place.
  if (brief) $('walk-body').scrollTop = top;
}
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
  if (!state.provider || !state.model) fragment.append(el('p','No model configured. Everything here works without one.','meta'));
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
    state.tour=tour; state.stop=0; state.paused=false; state.finished=false; state.sidebar='stops'; $('filter').value=''; $('tour-select').value=kind;
    if (!tour.stops.length) { state.tour=null; notice(tour.limitations.join(' ')); renderFiles(); updateTour(); return; }
    await visitStop(0); notice(tour.limitations.join(' '));
  } catch (error) { notice(error.message); }
}
async function visitStop(index) {
  if (!state.tour || index<0 || index>=state.tour.stops.length) return;
  state.stop=index; state.paused=false; state.finished=false; state.visiting=true;
  try {
    if (state.tour.id === 'changes') showChange(state.review.changes[index]);
    else await navigate(state.tour.stops[index].node);
  } finally { state.visiting=false; }
  updateTour();
}
// Next moves along the tour; in a review it also marks the stop being left as read.
function nextStop() {
  if (!state.tour) return;
  if (state.paused) { visitStop(state.stop); return; }
  const change = currentChange(); if (change && !state.read.has(change.id)) { state.read.add(change.id); persist(); }
  if (state.stop < state.tour.stops.length-1) visitStop(state.stop+1); else finishTour();
}
function previousStop() { if (state.tour && !state.paused && !state.finished && state.stop > 0) visitStop(state.stop-1); else if (state.finished) visitStop(state.stop); }
function finishTour() {
  state.finished=true; state.sequence++; renderFiles(); updateTour(); clearWalk();
  const review = state.review, read = review ? review.changes.filter(change => state.read.has(change.id)).length : 0;
  walkHead(`${state.tour.title} · done`, 'End of the tour');
  renderStory(review ? [[`You have walked through ${state.tour.stops.length} stops of ${review.base_label} → ${review.head_label}.`], [`${read} of ${review.changes.length} changed units are marked read.`]]
    : [[`That was the ${state.tour.title}: ${state.tour.stops.length} stops.`], ['Pick another tour above, follow a link from any stop, or browse the files grouped on the left.']]);
  const row = el('div', undefined, 'walk-actions'), again = el('button', 'Start over'); again.addEventListener('click', () => visitStop(0)); row.append(again);
  if (review) { const copy = el('button', 'Copy summary', 'accent'); copy.addEventListener('click', copySummary); const other = el('button', 'Review another change'); other.addEventListener('click', openReviewPicker); row.prepend(copy); row.append(other); }
  else { const other = el('button', 'Review a change'); other.addEventListener('click', openReviewPicker); row.append(other); }
  $('walk-extra').append(row);
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
  state.review=review; state.selection=selection; state.sidebar='stops'; $('filter').value='';
  state.tour={id:'changes', title:'Change Tour', limitations:review.limitations, stops:review.changes.map(change => ({node:change.node, cohort:change.cohort, reason:`${change.status} · ${change.cohort.replace(/^\d+ · /,'')}`, provenance:{provider:'Local Git comparison', path:change.node.path, start:change.node.start, end:change.node.end, detail}}))};
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
const openFromReview = node => (state.review.live && unitExists(node) ? navigate : openReviewed)(node);
function reviewedDestination(item) {
  const button = destination(item.node, item.reason, item.provenance, openFromReview);
  button.dataset.relationship = item.relationship; return button;
}
function showChange(change) {
  const sequence = ++state.sequence; state.request?.abort(); window.tlcrHighlight.cancel();
  state.orientation = null; state.path = change.node.path; state.changeView = change.node.id;
  const file = state.review.live && change.status !== 'removed' ? state.files.find(item => item.path === change.node.path) : null;
  state.unit = file?.units.find(unit => unit.id === change.node.unit_id) || null;
  $('filename').textContent = change.node.path; $('units').replaceChildren();
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
  const review = state.review, walk = window.tlcrWalk; clearWalk();
  const chapter = walk.chapters(state.tour.stops, 'changes').find(item => item.items.some(({index}) => index === state.stop));
  walkHead(`Change Tour · stop ${state.stop + 1} of ${review.changes.length}${chapter ? ' · ' + chapter.title : ''}`, walk.short(change.node.name));
  renderStory(walk.narrateChange(review, state.stop), openFromReview);
  const extra = $('walk-extra'), mark = el('button', state.read.has(change.id) ? 'Mark unread (x)' : 'Mark read (x)'); mark.id = 'mark-read'; mark.addEventListener('click', toggleRead);
  const note = el('textarea'); note.id = 'change-note'; note.maxLength = 4000; note.rows = 2; note.placeholder = 'Note to yourself about this unit'; note.setAttribute('aria-label', 'Note about this unit, kept in this browser only'); note.value = state.notes[change.node.id] || '';
  note.addEventListener('input', () => { const had = !!state.notes[change.node.id]; if (note.value.trim()) state.notes[change.node.id] = note.value; else delete state.notes[change.node.id]; persist(); if (had !== !!state.notes[change.node.id]) renderFiles(); });
  extra.append(note, mark);
  explainWithAI();
  const next = walk.upNext(state.tour, state.stop);
  if (next) { const label = el('p', undefined, 'up-next'); label.append(el('span', 'Up next  '), el('strong', next.name), el('small', ' · ' + next.path)); $('walk-upnext').append(label); }
  const facts = card('CHANGE'); facts.append(el('p',`${change.status} · ${change.cohort.replace(/^\d+ · /,'')} · +${change.added} −${change.removed}${change.whitespace_only ? ' · whitespace only' : ''}`));
  if (change.before && (change.status === 'renamed' || change.status === 'moved')) facts.append(el('p',`Was ${change.before.name} · ${change.before.path}:${change.before.start}`,'meta'));
  facts.append(el('p',`${review.base_label} → ${review.head_label}`,'meta'));
  const signals = card('AROUND THIS CHANGE','signals');
  for (const signal of change.signals) { signals.append(el('p',signal.detail)); for (const item of signal.related) signals.append(reviewedDestination(item)); }
  if (!change.signals.length) signals.append(el('p','No caller or test signals for this stop.','muted'));
  const relationships = card('RELATIONSHIP CHANGES');
  for (const [label, edges] of [['Added', change.relationships_added],['Removed',change.relationships_removed]]) for (const edge of edges) { relationships.append(el('p',`${label}: ${edge.kind} → ${edge.to}`)); provenance(relationships,edge.provenance); }
  if (!change.relationships_added.length && !change.relationships_removed.length) relationships.append(el('p','No resolved relationship changes for this stop.','muted'));
  const rest = el('div'); $('detail').append(facts,signals,relationships,rest);
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
  persist();
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

// The footer under the walkthrough: progress, Previous and Next, or a way back to the tour.
function updateTour() {
  const tour = state.tour, touring = !!tour?.stops.length, total = touring ? tour.stops.length : 0, away = touring && state.paused;
  $('walk-nav').hidden = !touring; $('tour-progress').hidden = !touring;
  if (!touring) return;
  const read = state.review ? state.review.changes.filter(change => state.read.has(change.id)).length : 0, at = state.finished ? total : state.stop + 1;
  $('tour-position').textContent = `${at} of ${total}${state.review ? ` · ${read} read` : ''}`;
  $('tour-bar').style.width = `${Math.round(100 * at / total)}%`;
  $('resume-tour').hidden = !away; $('resume-tour').textContent = `Back to stop ${state.stop + 1} (b)`;
  $('tour-prev').hidden = away; $('tour-next').hidden = away || state.finished;
  $('tour-prev').disabled = !state.finished && state.stop === 0;
  $('tour-next').textContent = state.stop === total - 1 ? 'Finish' : state.review && !state.read.has(state.review.changes[state.stop]?.id) ? 'Mark read, next →' : 'Next →';
}
async function refresh() {
  const previous=state.path, unit=state.unit?.id; ai.results.clear(); notice('Refreshing local analysis…'); state.request?.abort(); state.sequence++;
  try { await post('/api/refresh',{}); await loadRepository();
    if (state.review) {
      // Keep the selection: rebuild the review and return to the same unit when it is still part of it.
      const keep = state.review.changes[state.stop]?.node.id;
      try { await startReview(state.selection, keep); notice('Local map refreshed; review rebuilt.'); } catch (error) { endReview(); notice(error.message); }
      return;
    }
    state.tour=null; state.paused=false; renderFiles(); updateTour(); if (previous && state.files.some(f=>f.path===previous)) await openFile(previous,unit,false); else showOverview(); notice('Local map refreshed.'); } catch(error) {notice(error.message);}
}
// AI mode. Switching it on is the consent for this page session: from then on the
// code and reading brief of each stop you open are sent, every request is listed in
// the status bar, and the exact text of each is one click away. Off by default.
const ai = {on: false, log: [], results: new Map(), controller: null, used: 0, budget: 0, output: 0};
function aiAvailable() { return !!(state.provider && state.model); }
// The model and the host that receives requests: where code goes is always named.
function aiName() { return `${state.model} at ${state.destination || state.provider}`; }
function setAI(on) {
  ai.on = on && aiAvailable();
  if (!ai.on) { ai.controller?.abort(); $('walk-ai').replaceChildren(); }
  else notice(`AI is on: the code and reading brief of each stop you open are sent to ${aiName()}. Turn it off at any time.`);
  renderAIBar(); if (ai.on) explainWithAI();
}
function aiTarget() {
  const change = currentChange(), short = window.tlcrWalk.short;
  if (change) return {key: `change:${JSON.stringify(state.selection)}:${change.id}`, label: short(change.node.name), preview: '/api/review/explain/preview', url: '/api/review/explain', body: {...state.selection, change_id: change.id, enrich: true}, heading: 'AI description of this change'};
  if (state.unit && state.path) return {key: `unit:${state.path}:${state.unit.id}`, label: short(state.unit.name), preview: '/api/explain/preview', url: '/api/explain', body: {path: state.path, unit_id: state.unit.id, enrich: true}, heading: 'AI explanation'};
  return null;
}
function showAIResult(target, result) {
  $('walk-ai').replaceChildren(el('h3', target.heading, 'walk-label'), renderMarkdown(result.text), el('p', `Interpretation by a model, not a source fact · ${result.cached ? 'cached, no model call' : `${result.input_tokens} in · ${result.output_tokens} out`}`, 'meta'));
}
async function explainWithAI(again = false) {
  const target = ai.on && aiTarget(); if (!target) return;
  const sequence = state.sequence, box = $('walk-ai'), known = ai.results.get(target.key);
  if (known && !again) { showAIResult(target, known); return; }
  box.replaceChildren(el('h3', target.heading, 'walk-label'), el('p', 'Asking the model…', 'meta'));
  // A short pause, so stepping quickly through stops does not send a request for each.
  await new Promise(resolve => setTimeout(resolve, 600)); if (sequence !== state.sequence || !ai.on) return;
  const controller = ai.controller = new AbortController(), entry = {at: new Date(), label: target.label, kind: target.heading, status: 'preparing'};
  ai.log.unshift(entry); ai.log.length = Math.min(ai.log.length, 50); renderAIBar();
  try {
    const prepared = await post(target.preview, target.body, controller.signal);
    Object.assign(entry, {prompt: prepared.prompt, destination: prepared.destination, model: prepared.model, estimated: prepared.estimated_tokens});
    if (sequence !== state.sequence || !ai.on) { entry.status = 'not sent'; return; }
    entry.status = 'sending'; renderAIBar();
    const result = await post(target.url, {...target.body, digest: prepared.digest, approved: true}, controller.signal);
    Object.assign(entry, {status: result.cached ? 'cached' : 'sent', input: result.input_tokens, output: result.output_tokens});
    if (!result.cached) ai.output += result.output_tokens || 0;
    ai.results.set(target.key, result); if (sequence === state.sequence) showAIResult(target, result);
    const tree = await request('/api/tree'); ai.used = tree.used_input_tokens; ai.budget = tree.session_input_budget;
  } catch (error) {
    entry.status = error.name === 'AbortError' ? 'cancelled' : 'failed'; entry.error = error.message;
    if (sequence === state.sequence && error.name !== 'AbortError') box.replaceChildren(el('h3', target.heading, 'walk-label'), el('p', error.status === 404 || error.status === 409 ? error.message + ' Refresh to rebuild.' : error.message, 'warning'));
  } finally { renderAIBar(); }
}
// The status bar: what AI mode is doing and has cost, always in view while it is on.
function renderAIBar() {
  const toggle = $('ai-toggle'); toggle.disabled = !aiAvailable(); toggle.setAttribute('aria-pressed', String(ai.on)); toggle.textContent = ai.on ? 'AI on' : 'AI off';
  toggle.title = aiAvailable() ? (ai.on ? 'Stop sending stops to the model' : `Explain each stop with ${aiName()}`) : 'No model configured: add config.json beside the binary';
  document.body.classList.toggle('ai-on', ai.on); $('provider').hidden = ai.on; $('ai-bar').hidden = !ai.on; if (!ai.on) return;
  const done = ai.log.filter(entry => entry.status === 'sent'), cached = ai.log.filter(entry => entry.status === 'cached').length, busy = ai.log.find(entry => entry.status === 'preparing' || entry.status === 'sending');
  const last = ai.log.find(entry => entry.status === 'sent' || entry.status === 'cached'), failed = ai.log[0]?.status === 'failed' ? ai.log[0] : null;
  const share = ai.budget ? Math.min(100, Math.round(100 * ai.used / ai.budget)) : 0, meter = el('span', undefined, 'meter'), fill = el('span'); fill.style.width = share + '%'; meter.append(fill);
  const item = (label, value, className = '') => { const span = el('span', undefined, 'bar-item ' + className); span.append(el('small', label), el('strong', value)); return span; };
  const tokens = item('input', `${ai.used.toLocaleString()} / ${ai.budget.toLocaleString()}`); tokens.append(meter);
  const sent = el('button', `What was sent (${ai.log.filter(entry => entry.prompt).length})`); sent.addEventListener('click', openAILog);
  $('ai-bar').replaceChildren(
    item('AI', aiName(), 'bar-model'),
    item('requests', `${done.length} sent · ${cached} cached`), tokens, item('output', ai.output.toLocaleString()),
    item('last', last ? `${last.label} · ${last.status === 'cached' ? 'cached' : `${last.input} in → ${last.output} out`}` : '—'),
    item('now', busy ? `${busy.status === 'sending' ? 'sending' : 'preparing'} ${busy.label}…` : failed ? `failed: ${failed.error}` : 'idle', busy ? 'busy' : failed ? 'failed' : ''),
    sent);
}
function openAILog() {
  const list = $('ai-log-list'); list.replaceChildren();
  const show = entry => { $('ai-destination').textContent = entry.prompt ? `To ${entry.destination} · ${entry.model} · ${entry.status} · about ${entry.estimated} input tokens · ${entry.at.toLocaleTimeString()}` : ''; $('ai-prompt').value = entry.prompt || ''; for (const row of list.children) row.classList.toggle('active', row.entry === entry); };
  const entries = ai.log.filter(entry => entry.prompt);
  for (const entry of entries) { const row = el('button', undefined, 'choice'); row.entry = entry; row.append(el('strong', entry.label), el('small', `${entry.kind} · ${entry.status} · ${entry.at.toLocaleTimeString()}`)); row.addEventListener('click', () => show(entry)); list.append(row); }
  if (entries.length) show(entries[0]); else { list.append(el('p', 'Nothing has been sent yet.', 'muted')); $('ai-destination').textContent = ''; $('ai-prompt').value = ''; }
  $('ai-dialog').showModal();
}
const bindings = {'next':'j / n','previous':'k / p','back':'[','forward':']','search':'/ or g f','tour':'t','read-next':'r','tests':'g t','callers':'g c','dependencies':'g d','evidence':'w','explain':'e','review':'c','mark-read':'x','next-unread':'u','next-hunk':'J','previous-hunk':'K','toggle-diff':'v','resume':'b','help':'?','escape':'Esc'};
function renderHelp() { $('shortcut-list').replaceChildren(); for(const cmd of state.commands){const row=el('div',undefined,'shortcut');row.append(el('span',cmd.label),el('kbd',bindings[cmd.id]||'Menu'));$('shortcut-list').append(row);} }
function openDetails() { $('walk-details').open = true; }
function focusRelationship(kinds) { openDetails(); const target=[...document.querySelectorAll('#walk-body button[data-relationship]')].find(b=>kinds.includes(b.dataset.relationship)); if(target)target.focus();else notice('No resolved relationship of this kind at this stop.'); }
const actions={
 next:()=>state.tour&&!state.paused?nextStop():state.orientation?.next[0]&&navigate(state.orientation.next[0].node),previous:()=>state.tour&&!state.paused?previousStop():historyMove(-1),back:()=>historyMove(-1),forward:()=>historyMove(1),search:()=>$('filter').focus(),tour:()=>$('tour-select').focus(),
 'read-next':()=>(document.querySelector('#walk-upnext button')||(openDetails(),document.querySelector('#read-next button')))?.focus(),tests:()=>focusRelationship(['tested-by','tests']),callers:()=>focusRelationship(['called-by']),dependencies:()=>focusRelationship(['calls','local-module-source','imports']),
 evidence:()=>{openDetails();$('evidence')?.scrollIntoView({block:'start'});},explain:()=>ai.on?explainWithAI(true):notice(aiAvailable()?'Turn AI on in the header to explain each stop.':'No model configured.'),review:openReviewPicker,'mark-read':toggleRead,'next-unread':nextUnread,'next-hunk':()=>moveHunk(1),'previous-hunk':()=>moveHunk(-1),'toggle-diff':toggleDiff,resume:()=>state.tour&&visitStop(state.stop),help:()=>$('help-dialog').showModal(),escape:()=>{for(const d of document.querySelectorAll('dialog[open]'))d.close();}
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
$('tab-stops').addEventListener('click',()=>{state.sidebar='stops';renderFiles();});$('tab-files').addEventListener('click',()=>{state.sidebar='files';renderFiles();});
$('filter').addEventListener('input',renderFiles); $('refresh').addEventListener('click',refresh); $('home').addEventListener('click',showOverview);
$('back').addEventListener('click',()=>historyMove(-1));$('forward').addEventListener('click',()=>historyMove(1));$('start-tour').addEventListener('click',()=>startTour());
$('tour-prev').addEventListener('click',previousStop);$('tour-next').addEventListener('click',nextStop);$('resume-tour').addEventListener('click',()=>visitStop(state.stop));
$('help').addEventListener('click',actions.help);$('close-help').addEventListener('click',()=>$('help-dialog').close());
$('close-ai').addEventListener('click',()=>$('ai-dialog').close());$('ai-toggle').addEventListener('click',()=>setAI(!ai.on));
loadRepository().then(showOverview).catch(error=>notice(error.message));
