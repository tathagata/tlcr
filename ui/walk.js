'use strict';
// Pure helpers for the guided walkthrough: grouping for the sidebar and the
// plain-language narration beside the code. Nothing here touches the DOM.
// A paragraph is a list of parts: a string, or {code, node} for a named unit
// (node, when present, makes it a link).
window.tlcrWalk = (() => {
  // "func main" reads as main; "method App.Routes" as App.Routes.
  const short = name => String(name || '').replace(/^(func|method|type|resource|data|module|variable|output|class|def|play|task) /, '');
  const code = (node, name) => ({code: short(name ?? node?.name), node: node || null});
  const cohortName = cohort => String(cohort || '').replace(/^\d+ · /, '');
  const plural = (count, word) => `${count} ${word}${count === 1 ? '' : 's'}`;
  function list(items) {
    const out = [];
    items.forEach((item, index) => { if (index) out.push(index === items.length - 1 ? ' and ' : ', '); out.push(item); });
    return out;
  }
  function dirname(path) { const at = path.lastIndexOf('/'); return at < 0 ? '' : path.slice(0, at); }

  // Files grouped by the directory that holds them, root first. Entry points
  // and tests are named so a reader can tell what a folder is for.
  function folders(files, entryPaths = []) {
    const entries = new Set(entryPaths), groups = new Map();
    for (const file of files) {
      const dir = dirname(file.path);
      if (!groups.has(dir)) groups.set(dir, {dir, files: []});
      groups.get(dir).files.push(file);
    }
    return [...groups.values()].sort((a, b) => (a.dir === '') === (b.dir === '') ? a.dir.localeCompare(b.dir) : a.dir === '' ? -1 : 1).map(group => {
      const tests = group.files.filter(file => /(_test\.go|\.test\.[cm]?[jt]sx?|^test_.*\.py|_test\.py)$/.test(file.path.split('/').pop())).length;
      const notes = [plural(group.files.length, 'file')];
      if (group.files.some(file => entries.has(file.path))) notes.push('entry point');
      if (tests && tests === group.files.length) notes.push('tests'); else if (tests) notes.push(plural(tests, 'test file'));
      return {...group, label: group.dir || 'Repository root', detail: notes.join(' · ')};
    });
  }

  // Tour stops in runs that share why they are on the tour: one chapter per
  // starting point, per unit whose calls are being followed, or per cohort.
  function chapterKey(stop, tourID) {
    if (tourID === 'changes') return stop.cohort || stop.reason;
    const by = /^(Called by|Local implementation referenced by|Handler registered by) (.+)$/.exec(stop.reason);
    if (by) return 'from:' + by[2];
    if (stop.reason.startsWith('Additional structural context')) return 'more';
    if (stop.reason === 'Production unit directly called by a test' || stop.reason.startsWith('Test directly calls ')) return 'pairs';
    return 'start';
  }
  function chapterTitle(key, stop, tourID) {
    if (tourID === 'changes') return cohortName(key);
    if (key.startsWith('from:')) return `What ${short(key.slice(5))} uses`;
    return {start: tourID === 'testing' ? 'Tests' : tourID === 'state' ? 'Where state lives' : tourID === 'recent' ? 'Recently changed' : 'Where it starts', more: 'Elsewhere of note', pairs: 'Code and the tests that call it'}[key];
  }
  function chapters(stops, tourID) {
    const out = [];
    stops.forEach((stop, index) => {
      const key = chapterKey(stop, tourID);
      if (!out.length || out[out.length - 1].key !== key) out.push({key, title: chapterTitle(key, stop, tourID), items: []});
      out[out.length - 1].items.push({index, stop});
    });
    return out;
  }

  const tourIntros = {
    architecture: 'This tour starts where the program starts and follows the code it calls, one step at a time, then visits other units that stand out.',
    execution: 'This tour follows execution: from each entry point to the code it calls, and on from there.',
    state: 'This tour visits the places that read or hold configuration and stored state.',
    testing: 'This tour pairs production code with the tests that call it directly. The tests show what the code is expected to do.',
    recent: 'This tour visits code in files that changed in the latest local commits.',
  };
  const roleSentences = {
    'entry point': 'Execution starts here.',
    'network boundary': 'This is where the program meets the network.',
    'storage boundary': 'This reads or writes files or stored data.',
    configuration: 'This reads configuration from the environment.',
    'test support': 'This is test support code.',
  };

  // Why the reader is at this stop, in the voice of someone showing them around.
  // `around` is the orientation for the unit once it has loaded, and `brief` its
  // reading brief; either may be null while loading.
  function narrate(tour, index, around, brief = null) {
    const stop = tour.stops[index], node = stop.node, out = [], previous = tour.stops[index - 1];
    if (index === 0 && tourIntros[tour.id]) out.push([tourIntros[tour.id]]);
    const by = /^(Called by|Local implementation referenced by|Handler registered by) (.+)$/.exec(stop.reason);
    if (by) {
      const parent = tour.stops.find(item => item.node.name === by[2])?.node;
      const verb = {'Called by': 'calls', 'Local implementation referenced by': 'points to', 'Handler registered by': 'registers'}[by[1]];
      const since = previous && previous.node.name === by[2] ? 'the stop before this one' : null;
      out.push([code(parent, by[2]), ` ${verb} `, code(null, node.name), since ? `, so this is the next step down from ${since}.` : `. We are following what ${short(by[2])} uses.`]);
    } else if (stop.reason.startsWith('Test directly calls ')) {
      out.push(['This test calls ', code(previous?.node.name === stop.reason.slice(20) ? previous.node : null, stop.reason.slice(20)), ' directly. Its assertions show the behaviour the author expects.']);
    } else if (stop.reason === 'Production unit directly called by a test') {
      out.push([code(null, node.name), ' is called directly by a test, which is the clearest record of what it should do. The test comes next.']);
    } else if (stop.reason.startsWith('Test declaration')) {
      out.push(['A test. Read its assertions for the behaviour it pins down.']);
    } else if (stop.reason.startsWith('Unit in a recently changed file')) {
      out.push([code(null, node.name), ` lives in ${node.path}, which changed in the latest local commits.`]);
    } else {
      const more = stop.reason.startsWith('Additional structural context');
      out.push([more ? 'Off the main path, ' : '', code(null, node.name), ` in ${node.path}.`]);
    }
    for (const role of node.roles || []) if (roleSentences[role] && !(role === 'entry point' && stop.reason.startsWith('Test'))) out[out.length - 1].push(' ' + roleSentences[role]);
    return [...out, ...describe(around, brief)];
  }
  // Connections, then what the brief adds, for any unit.
  function describe(around, brief) {
    const tests = (brief?.facts || []).some(fact => fact.kind === 'test-states');
    return [...(around ? connections(around, {tests: !tests}) : []), ...narrateBrief(brief)];
  }

  // What a unit connects to, from resolved local relationships only.
  // `tests: false` leaves tests to the brief, which says what each one checks.
  function connections(around, {tests: withTests = true} = {}) {
    const out = [], names = (edges, side) => edges.slice(0, 4).map(edge => code(edge.node || null, edge.node?.name ?? edge[side].replace(/^unit:[^:]*:/, '')));
    const byNode = new Map((around.next || []).map(item => [item.node.id, item.node]));
    const resolve = (edges, side) => edges.map(edge => ({...edge, node: byNode.get(edge[side]) || null}));
    const tests = resolve(around.outgoing.filter(edge => edge.kind === 'tested-by'), 'to'), testIDs = new Set(tests.map(edge => edge.to));
    // A test that calls the unit is named once, as a test.
    const callers = resolve(around.incoming.filter(edge => edge.kind === 'calls' && !testIDs.has(edge.from)), 'from');
    const calls = resolve(around.outgoing.filter(edge => edge.kind === 'calls' || edge.kind === 'local-module-source'), 'to');
    const more = (edges) => edges.length > 4 ? [` and ${edges.length - 4} more`] : [];
    if (calls.length) out.push(['It uses ', ...list(names(calls, 'to')), ...more(calls), ' in this repository.']);
    if (callers.length) out.push([callers.length === 1 ? 'One place calls it: ' : `${callers.length} places call it: `, ...list(names(callers, 'from')), ...more(callers), '.']);
    else if (!around.node.roles?.includes('entry point') && !around.node.roles?.includes('test')) out.push(['Nothing in the indexed source calls it directly.']);
    if (withTests && tests.length) out.push(['Tested directly by ', ...list(names(tests, 'to')), ...more(tests), '.']);
    return out;
  }

  const cohortIntros = {
    'types and contracts': 'First, the types and contracts. Everything else in the change is shaped by these.',
    'implementation and file context': 'Now the implementation.',
    tests: 'Then the tests that came with the change.',
    documentation: 'Finally, the documentation.',
  };
  const edgeVerbs = {calls: 'calls', 'local-module-source': 'uses the local module', 'tested-by': 'is tested by', 'route-to-handler': 'routes to'};

  // A change stop, told as what changed and what it touches.
  function narrateChange(review, index) {
    const change = review.changes[index], node = change.node, out = [], previous = review.changes[index - 1];
    if (index === 0) out.push([`This change touches ${plural(review.files, 'file')} (+${review.added} −${review.removed}). We will go through it contracts first, then the implementation, tests and documentation.`]);
    const cohort = cohortName(change.cohort);
    if ((!previous || previous.cohort !== change.cohort) && cohortIntros[cohort]) out.push([cohortIntros[cohort]]);
    const lines = `${plural(change.added, 'line')} added, ${change.removed} removed`;
    const what = {
      added: [code(null, node.name), ` is new (${plural(change.added, 'line')}).`],
      removed: [code(null, node.name), ` was removed (${plural(change.removed, 'line')}).`],
      renamed: [code(null, change.before?.name), ' was renamed to ', code(null, node.name), ` (${lines}).`],
      moved: [code(null, node.name), ` moved from ${change.before?.path} to ${node.path} (${lines}).`],
      'file context changed': [`Text outside any unit changed in ${node.path} (${lines}).`],
    }[change.status] || [code(null, node.name), ` changed: ${lines}.`];
    if (change.whitespace_only) what.push(' Only whitespace differs.');
    out.push(what);
    for (const signal of change.signals || []) out.push(signalSentence(signal, change.status));
    // Everything a new unit relates to is new, and its signals already name its tests.
    if (change.status === 'added') return out;
    const target = edge => code(null, edge.to.replace(/^unit:[^:]*:/, '').replace(/^file:/, ''));
    const tested = (change.signals || []).some(signal => signal.kind === 'tests-changed' || signal.kind === 'tests-unchanged');
    for (const [edges, now] of [[change.relationships_added || [], true], [change.relationships_removed || [], false]]) {
      const kinds = [...new Set(edges.map(edge => edge.kind))];
      for (const kind of kinds) {
        if (kind === 'tested-by' && now && tested) continue;
        const names = list(edges.filter(edge => edge.kind === kind).map(target));
        out.push(kind === 'tested-by' ? [now ? 'It is now tested by ' : 'It is no longer tested by ', ...names, '.'] : [now ? 'It now ' : 'It no longer ', edgeVerbs[kind] || kind, ' ', ...names, '.']);
      }
    }
    return out;
  }
  // Caller and test facts read as sentences that name the units involved.
  function signalSentence(signal, status) {
    const related = signal.related || [], names = list(related.map(item => code(item.node))), many = related.length !== 1;
    switch (signal.kind) {
      case 'unchanged-callers': return [status === 'removed' ? 'Before this change it was called from ' : 'It is called from ', ...names, `, which ${many ? 'were' : 'was'} not changed.`];
      case 'tests-changed': return [many ? 'Its tests ' : 'Its test ', ...names, ` changed alongside it.`];
      case 'tests-unchanged': return [...names, ` ${many ? 'test' : 'tests'} it directly and ${many ? 'were' : 'was'} not changed.`];
      case 'no-known-test': return ['No test calls it directly.'];
      default: return [signal.detail.replace(/\.?$/, '.')];
    }
  }

  // A brief fact's unit as a link target; ids look like unit:<path>:<name>.
  function briefNode(fact, name) {
    if (!fact.node_id?.startsWith('unit:')) return null;
    return {id: fact.node_id, unit_id: fact.node_id.slice(5), name, path: fact.provenance.path, start: fact.provenance.start};
  }
  // What the tests, the history of these lines and the comment above them say,
  // most reliable first. Each sentence says how far it can be trusted.
  function narrateBrief(brief) {
    const out = [], facts = kind => (brief?.facts || []).filter(fact => fact.kind === kind);
    for (const fact of facts('test-states')) {
      const name = fact.node_id ? fact.node_id.replace(/^unit:[^:]*:/, '') : '';
      out.push(['Its test ', code(briefNode(fact, name), name), ` is named for what it checks: “${fact.text}”.`]);
    }
    if (facts('uncommitted').length) out.push(['These lines have uncommitted changes; the history below is for the committed version.']);
    const commits = facts('commit');
    if (commits.length) {
      out.push([`These lines last changed on ${commits[0].date}: “${commits[0].text}”.`]);
      if (commits.length > 1) out.push(['Before that: ', ...list(commits.slice(1, 3).map(fact => `${fact.date} “${fact.text}”`)), '.']);
    }
    for (const fact of facts('comment')) {
      out.push([{
        'comment-current': `The comment above it was written on ${fact.date}, no earlier than the code it describes: `,
        'comment-older-than-code': `The comment above it dates from ${fact.date}, before the code last changed, so it may be out of date: `,
      }[fact.reliability] || 'The comment above it, with no history to check its age: ', `“${fact.text}”`]);
    }
    return out;
  }

  // The stop after `index`, as a short preview.
  function upNext(tour, index) {
    const next = tour.stops[index + 1];
    return next ? {index: index + 1, node: next.node, name: short(next.node.name), path: next.node.path} : null;
  }

  return {short, folders, chapters, narrate, describe, connections, narrateChange, narrateBrief, upNext};
})();
