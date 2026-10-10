'use strict';
// Pure helpers for review mode. Nothing here touches the DOM, so the picker's
// matching and the diff's source preservation are tested directly.
window.tlcrReview = (() => {
  // "A..B" names two sides; any other text is one commit against its parent.
  function parseRevision(text) {
    const value = text.trim();
    if (!value || value.length > 256 || /\s/.test(value)) return null;
    const at = value.indexOf('..');
    if (at < 0) return {commit: value};
    const base = value.slice(0, at), head = value.slice(at + 2);
    return base && head && !head.startsWith('.') ? {base, head} : null;
  }
  function query(selection) {
    const params = new URLSearchParams();
    for (const key of ['base', 'head', 'commit']) if (selection[key]) params.set(key, selection[key]);
    return params.toString();
  }
  // Listed changes that match come first; a typed revision is always last, so
  // filtering by words in a subject never selects it by accident.
  function choices(list, text) {
    const needle = text.trim().toLowerCase(), out = [];
    for (const [section, changes] of [['Working tree', list.working], ['Recent commits', list.commits], ['Branches against ' + list.default_branch, list.branches]])
      for (const change of changes || [])
        if (!needle || `${change.title} ${change.id || ''} ${change.detail} ${change.kind}`.toLowerCase().includes(needle)) out.push({section, change});
    const selection = parseRevision(text);
    if (selection) out.push({section: 'Revision', change: {kind: 'revision', title: text.trim(), detail: selection.commit ? 'One commit against its first parent' : `From ${selection.base} to ${selection.head}`, selection}});
    return out;
  }
  // One row per hunk header and per line. Tokens are used only when they
  // reproduce the line exactly; otherwise the row falls back to plain text.
  function rows(change, beforeTokens, afterTokens) {
    const out = [], beforeStart = change.before ? change.before.start : 1, afterStart = change.node.start;
    for (const hunk of change.hunks) {
      const first = hunk.lines[0], before = hunk.lines.find(line => line.before), after = hunk.lines.find(line => line.after);
      if (first) out.push({hunk: `@@ −${before ? before.before : 0} +${after ? after.after : 0} @@`});
      for (const line of hunk.lines) {
        const tokens = line.op === '-' ? beforeTokens?.[line.before - beforeStart] : afterTokens?.[line.after - afterStart];
        const exact = Array.isArray(tokens) && tokens.map(token => token.text).join('') === line.text;
        out.push({op: line.op, text: line.text, before: line.before || 0, after: line.after || 0, tokens: exact ? tokens : null});
      }
    }
    return out;
  }
  function groups(changes) {
    const out = [];
    changes.forEach((change, index) => {
      if (!out.length || out[out.length - 1].cohort !== change.cohort) out.push({cohort: change.cohort, items: []});
      out[out.length - 1].items.push({index, change});
    });
    return out;
  }
  // The next stop after `from` that is not marked read, wrapping around.
  function nextUnread(changes, read, from) {
    for (let step = 1; step <= changes.length; step++) {
      const index = (from + step) % changes.length;
      if (!read.has(changes[index].id)) return index;
    }
    return -1;
  }
  // Read marks and notes are kept per repository in browser storage only.
  // Storage can be missing, full or hold anything: every failure means "nothing kept".
  const storageKey = repository => 'tlcr.review.' + repository;
  function load(storage, repository) {
    const empty = {read: new Set(), notes: {}};
    try {
      const data = JSON.parse(storage.getItem(storageKey(repository)) || 'null');
      if (!data || typeof data !== 'object') return empty;
      const read = new Set((Array.isArray(data.read) ? data.read : []).filter(id => typeof id === 'string').slice(-5000)), notes = {};
      for (const [id, text] of Object.entries(data.notes && typeof data.notes === 'object' ? data.notes : {}).slice(-500)) if (typeof text === 'string' && text) notes[id] = text.slice(0, 4000);
      return {read, notes};
    } catch { return empty; }
  }
  function save(storage, repository, read, notes) {
    try { storage.setItem(storageKey(repository), JSON.stringify({read: [...read].slice(-5000), notes})); return true; } catch { return false; }
  }
  function clear(storage, repository) { try { storage.removeItem(storageKey(repository)); } catch { /* nothing kept */ } }
  // A Markdown checklist of the review: what was read, the facts beside each stop, and notes.
  function summary(review, read, notes) {
    const done = review.changes.filter(change => read.has(change.id)).length;
    const lines = [`# Review: ${review.base_label} → ${review.head_label}`, '', `${review.files} files · +${review.added} −${review.removed} · ${done} of ${review.changes.length} stops read`];
    for (const group of groups(review.changes)) {
      lines.push('', '## ' + group.cohort.replace(/^\d+ · /, ''));
      for (const {change} of group.items) {
        lines.push(`- [${read.has(change.id) ? 'x' : ' '}] \`${change.node.name}\` · ${change.node.path}:${change.node.start} · ${change.status} · +${change.added} −${change.removed}`);
        for (const signal of change.signals || []) lines.push('  - ' + signal.detail);
        const note = notes[change.node.id];
        if (note) lines.push(...note.trim().split('\n').map((text, index) => (index ? '    ' : '  - Note: ') + text));
      }
    }
    return lines.join('\n') + '\n';
  }
  return {parseRevision, query, choices, rows, groups, nextUnread, load, save, clear, summary};
})();
