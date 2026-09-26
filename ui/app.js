'use strict';
const $ = id => document.getElementById(id);
const state = { files: [], overview: null, commands: [], path: null, unit: null, source: '', sourceLines: [], highlighted: null, codeWindow: 0, entry: null, orientation: null, sequence: 0, history: [], cursor: -1, tour: null, review: null, changeView: null, stop: 0, paused: false, prepared: null, provider: '', model: '', request: null };
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
function destination(node, reason, p) {
  const button = el('button', undefined, 'destination'); button.append(el('strong', node.name), el('small', `${node.path}:${node.start || 1} · ${reason}`));
  if (p) button.title = `${p.provider}: ${p.detail} (${sourceLabel(p)})`;
  button.addEventListener('click', () => navigate(node)); return button;
}
function card(title, id) { const box = el('div', undefined, 'card'); if (id) box.id = id; box.append(el('h3', title)); return box; }
async function loadRepository() {
  const [tree, overview, commands] = await Promise.all([request('/api/tree'), request('/api/overview'), request('/api/commands')]);
  state.files = tree.files; state.overview = overview; state.commands = commands; state.provider = tree.provider; state.model = tree.model;
  $('provider').textContent = tree.provider && tree.model ? `AI available · ${tree.provider} / ${tree.model}` : 'Local analysis · AI optional';
  $('budget').textContent = tree.provider ? `${tree.used_input_tokens} / ${tree.session_input_budget} input tokens` : '';
  $('count').textContent = `${tree.files.length} files`; renderFiles(); renderHelp();
}
function renderFiles() {
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
function showOverview() {
  state.sequence++; state.request?.abort(); window.tlcrHighlight.cancel(); state.path = null; state.unit = null; state.changeView = null; state.orientation = null;
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
  $('detail').append(guidance, limits, privacy);
}
async function navigate(node, history = true) {
  if (node.kind === 'package') { notice('Choose a source file in this package.'); return; }
  await openFile(node.path, node.unit_id || null, history);
}
async function openFile(path, unitID = null, history = true) {
  const sequence = ++state.sequence; state.request?.abort(); state.request = new AbortController(); const signal = state.request.signal;
  notice(); state.prepared = null; state.orientation = null; state.changeView = null;
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
function renderOrientation(data) {
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
  $('detail').replaceChildren(fragment); document.querySelector('.context-body').scrollTop=0;
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
    let tour;
    if (kind === 'changes') { state.review = await request('/api/review?base='+encodeURIComponent($('review-base').value)); tour = state.review.tour; }
    else { state.review = null; tour = await request('/api/tour?kind='+encodeURIComponent(kind)); }
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
function showChange(change) {
  state.sequence++; state.request?.abort(); window.tlcrHighlight.cancel();
  state.prepared = null; state.orientation = null; state.unit = null; state.path = change.node.path; state.changeView = change.node.id;
  $('filename').textContent = change.node.path; $('filekind').textContent = `${change.status} · local comparison`;
  $('unitlabel').textContent = change.node.name; $('units').replaceChildren(); renderFiles();
  const view = el('div', undefined, 'change-source');
  for (const [title, source, start] of [[`BASE · ${state.review.base.slice(0,12)}`, change.before_source, change.before?.start || 1], ['INDEXED WORKING TREE', change.after_source, change.node.start]]) {
    const part = el('section'); part.append(el('h3',`${title} · ${change.node.path}:${start}`),el('pre', source || '(No source on this side)', 'change-code')); view.append(part);
  }
  $('code').replaceChildren(view); $('code').scrollTop = 0;
  const facts = card('CHANGE EVIDENCE'); facts.append(el('h3',change.node.name),el('p',`${change.status} · ${change.cohort}`),el('p',`${state.review.files} changed files · ${state.review.generated_files} generated files collapsed`));
  const relationships = card('RELATIONSHIP CHANGES');
  for (const [label, edges] of [['Added', change.relationships_added],['Removed',change.relationships_removed]]) for (const edge of edges) { relationships.append(el('p',`${label}: ${edge.kind} → ${edge.to}`)); provenance(relationships,edge.provenance); }
  if (!change.relationships_added.length && !change.relationships_removed.length) relationships.append(el('p','No resolved relationship changes for this source stop.','muted'));
  const related = card('AFFECTED NEIGHBORS','read-next');
  for (const item of change.related) {
    const exists = state.files.some(file => file.path === item.node.path && (!item.node.unit_id || file.units.some(unit => unit.id === item.node.unit_id)));
    if (exists) { const button = destination(item.node,item.reason,item.provenance); button.dataset.relationship = item.relationship; related.append(button); }
    else related.append(el('p',`${item.reason}: ${item.node.name} · ${item.node.path} (base only)`,'muted'));
  }
  const current = state.files.find(file => file.path === change.node.path);
  if (current && change.status !== 'removed') { const button = el('button','Read current source and evidence'); button.addEventListener('click',()=>openFile(current.path, change.node.unit_id || null)); facts.append(button); }
  $('detail').replaceChildren(facts,relationships,related); document.querySelector('.context-body').scrollTop=0;
}

function updateTour() {
  const active=!!state.tour?.stops.length && !state.paused;
  $('tour-progress').hidden=!active; $('resume-tour').hidden=!state.tour?.stops.length || !state.paused;
  $('tour-position').textContent=state.tour ? `${state.stop+1} / ${state.tour.stops.length}` : '';
  $('tour-prev').disabled=!active || state.stop===0; $('tour-next').disabled=!active || state.stop===state.tour.stops.length-1; updateStopReason();
}
function updateStopReason() {
  const stop=state.tour?.stops[state.stop]; const visible=stop && !state.paused && (state.changeView === stop.node.id || (stop.node.unit_id===state.unit?.id && stop.node.path===state.path));
  $('stop-reason').hidden=!visible; if (visible) { $('stop-reason').replaceChildren(el('strong',`${state.tour.title} · ${state.stop+1}/${state.tour.stops.length} — ${stop.reason}`)); provenance($('stop-reason'),stop.provenance); }
}
async function refresh() {
  const previous=state.path, unit=state.unit?.id; notice('Refreshing local analysis…'); state.request?.abort(); state.sequence++;
  try { await post('/api/refresh',{}); await loadRepository(); state.tour=null; state.paused=false; updateTour(); if (previous && state.files.some(f=>f.path===previous)) await openFile(previous,unit,false); else showOverview(); notice('Local map refreshed.'); } catch(error) {notice(error.message);}
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
const bindings = {'next':'j / n','previous':'k / p','back':'[','forward':']','search':'/ or g f','tour':'t','read-next':'r','tests':'g t','callers':'g c','dependencies':'g d','evidence':'w','explain':'e','help':'?','escape':'Esc'};
function renderHelp() { $('shortcut-list').replaceChildren(); for(const cmd of state.commands){const row=el('div',undefined,'shortcut');row.append(el('span',cmd.label),el('kbd',bindings[cmd.id]||'Menu'));$('shortcut-list').append(row);} }
function focusRelationship(kinds) { const target=[...document.querySelectorAll('#read-next button')].find(b=>kinds.includes(b.dataset.relationship)); if(target)target.focus();else notice('No resolved relationship of this kind at this stop.'); }
const actions={
 next:()=>state.tour&&!state.paused?visitStop(state.stop+1):state.orientation?.next[0]&&navigate(state.orientation.next[0].node),previous:()=>state.tour&&!state.paused?visitStop(state.stop-1):historyMove(-1),back:()=>historyMove(-1),forward:()=>historyMove(1),search:()=>$('filter').focus(),tour:()=>$('tour-select').focus(),
 'read-next':()=>document.querySelector('#read-next button')?.focus(),tests:()=>focusRelationship(['tested-by','tests']),callers:()=>focusRelationship(['called-by']),dependencies:()=>focusRelationship(['calls','local-module-source','imports']),
 evidence:()=>$('evidence')?.scrollIntoView({block:'start'}),explain:previewAI,help:()=>$('help-dialog').showModal(),escape:()=>{for(const d of document.querySelectorAll('dialog[open]'))d.close();}
};
let chord=false, chordTimer;
document.addEventListener('keydown',event=>{
 if(event.metaKey||event.ctrlKey||event.altKey||event.isComposing)return;
 if(event.key==='Escape'){chord=false;return;}
 if(event.target.closest('input,textarea,select,[contenteditable="true"]')||document.querySelector('dialog[open]'))return;
 const key=event.key;let command;
 if(chord){chord=false;clearTimeout(chordTimer);command={f:'search',t:'tests',c:'callers',d:'dependencies'}[key];}
 else if(key==='g'){chord=true;chordTimer=setTimeout(()=>{chord=false;},1000);event.preventDefault();return;}
 else command={j:'next',n:'next',k:'previous',p:'previous','[':'back',']':'forward','/':'search',t:'tour',r:'read-next',w:'evidence',e:'explain','?':'help'}[key];
 if(command){event.preventDefault();actions[command]();}
});
$('tour-select').addEventListener('change',()=>{ $('base-label').hidden = $('tour-select').value !== 'changes'; });
$('filter').addEventListener('input',renderFiles); $('refresh').addEventListener('click',refresh); $('home').addEventListener('click',showOverview);
$('back').addEventListener('click',()=>historyMove(-1));$('forward').addEventListener('click',()=>historyMove(1));$('start-tour').addEventListener('click',()=>startTour());
$('tour-prev').addEventListener('click',()=>visitStop(state.stop-1));$('tour-next').addEventListener('click',()=>visitStop(state.stop+1));$('pause-tour').addEventListener('click',()=>{state.paused=true;updateTour();});$('resume-tour').addEventListener('click',()=>visitStop(state.stop));
$('help').addEventListener('click',actions.help);$('close-help').addEventListener('click',()=>$('help-dialog').close());
$('close-ai').addEventListener('click',()=>$('ai-dialog').close());$('cancel-ai').addEventListener('click',()=>$('ai-dialog').close());$('send-ai').addEventListener('click',sendAI);
loadRepository().then(showOverview).catch(error=>notice(error.message));
