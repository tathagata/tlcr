const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const context = vm.createContext({}); context.window = context;
vm.runInContext(fs.readFileSync(path.join(__dirname, '../ui/walk.js'), 'utf8'), context);
const walk = context.tlcrWalk;
const plain = value => JSON.parse(JSON.stringify(value));
// A paragraph as the text a reader sees.
const text = parts => parts.map(part => typeof part === 'string' ? part : part.code).join('');
const node = (name, file, extra = {}) => ({id: `unit:${file}:${name}`, unit_id: `${file}:${name}`, name, path: file, start: 1, roles: [], ...extra});

test('unit names drop their kind prefix', () => {
  assert.equal(walk.short('func main'), 'main');
  assert.equal(walk.short('method App.Routes'), 'App.Routes');
  assert.equal(walk.short('resource aws_s3_bucket.logs'), 'aws_s3_bucket.logs');
  assert.equal(walk.short('README.md'), 'README.md');
});

test('files group by directory, root first, and say what each holds', () => {
  const files = [{path: 'internal/core/index.go'}, {path: 'main.go'}, {path: 'internal/core/index_test.go'}, {path: 'ui-tests/a.test.cjs'}, {path: 'docs/ROADMAP.md'}];
  const groups = walk.folders(files, ['main.go']);
  assert.deepEqual(plain(groups.map(group => group.label)), ['Repository root', 'docs', 'internal/core', 'ui-tests']);
  assert.equal(groups[0].detail, '1 file · entry point');
  assert.equal(groups[2].detail, '2 files · 1 test file');
  assert.equal(groups[3].detail, '1 file · tests');
});

test('tour chapters follow why each stop is there, in tour order', () => {
  const stops = [
    {node: node('func main', 'main.go'), reason: 'Entry point declared in source · 2 local dependencies'},
    {node: node('func listen', 'main.go'), reason: 'Called by func main'},
    {node: node('func Scan', 'index.go'), reason: 'Called by func main'},
    {node: node('func port', 'main.go'), reason: 'Called by func listen'},
    {node: node('func Other', 'other.go'), reason: 'Additional structural context · 1 production callers'},
  ];
  const chapters = walk.chapters(stops, 'architecture');
  assert.deepEqual(plain(chapters.map(chapter => [chapter.title, chapter.items.map(item => item.index)])), [['Where it starts', [0]], ['What main uses', [1, 2]], ['What listen uses', [3]], ['Elsewhere of note', [4]]]);
  const review = walk.chapters([{cohort: '1 · types and contracts', node: node('type A', 'a.go')}, {cohort: '3 · tests', node: node('func TestA', 'a_test.go')}], 'changes');
  assert.deepEqual(plain(review.map(chapter => chapter.title)), ['types and contracts', 'tests']);
});

test('a stop is narrated from its reason and the relationships around it', () => {
  const main = node('func main', 'main.go', {roles: ['entry point']}), scan = node('func Scan', 'index.go'), test = node('func TestScan', 'index_test.go');
  const tour = {id: 'architecture', stops: [{node: main, reason: 'Entry point declared in source'}, {node: scan, reason: 'Called by func main'}]};
  const first = walk.narrate(tour, 0, null).map(text);
  assert.match(first[0], /starts where the program starts/);
  assert.equal(first[1], 'main in main.go. Execution starts here.');
  const around = {node: scan, incoming: [{kind: 'calls', from: main.id, to: scan.id}], outgoing: [{kind: 'tested-by', from: scan.id, to: test.id}, {kind: 'calls', from: scan.id, to: 'unit:walk.go:func walkTree'}], next: [{node: main}, {node: test}]};
  const second = walk.narrate(tour, 1, around);
  assert.deepEqual(plain(second.map(text)), ['main calls Scan, so this is the next step down from the stop before this one.', 'It uses walkTree in this repository.', 'One place calls it: main.', 'Tested directly by TestScan.']);
  // The calling unit is a link back to its stop; an unresolved name is not a link.
  assert.equal(second[0][0].node.id, main.id);
  assert.equal(second[1][1].node, null);
});

test('an uncalled unit says so, but an entry point or test does not', () => {
  const lone = {node: node('func lone', 'a.go'), incoming: [], outgoing: [], next: []};
  assert.deepEqual(plain(walk.connections(lone).map(text)), ['Nothing in the indexed source calls it directly.']);
  assert.deepEqual(plain(walk.connections({...lone, node: node('func main', 'main.go', {roles: ['entry point']})})), []);
  // A test that calls the unit is named as a test, not also as a caller.
  const tested = {node: node('func listen', 'main.go'), incoming: [{kind: 'calls', from: 'unit:main.go:func main'}, {kind: 'calls', from: 'unit:main_test.go:func TestListen'}], outgoing: [{kind: 'tested-by', to: 'unit:main_test.go:func TestListen'}], next: []};
  assert.deepEqual(plain(walk.connections(tested).map(text)), ['One place calls it: main.', 'Tested directly by TestListen.']);
});

test('a change is told as what changed, what touches it and what it now relates to', () => {
  const caller = node('func Use', 'use.go'), testNode = node('func TestScan', 'index_test.go');
  const review = {files: 2, added: 9, removed: 3, changes: [
    {cohort: '2 · implementation and file context', status: 'modified', added: 5, removed: 2, node: node('func Scan', 'index.go'), signals: [
      {kind: 'unchanged-callers', detail: '1 caller not changed in this review', related: [{node: caller}]},
      {kind: 'tests-changed', detail: '1 directly related test changed in this review', related: [{node: testNode}]},
    ], relationships_added: [{kind: 'calls', to: 'unit:walk.go:func walkTree'}, {kind: 'calls', to: 'unit:walk.go:func skip'}, {kind: 'tested-by', to: testNode.id}], relationships_removed: [{kind: 'tested-by', to: 'unit:old_test.go:func TestOld'}]},
    {cohort: '3 · tests', status: 'added', added: 4, removed: 0, node: testNode, signals: [], relationships_added: [{kind: 'calls', to: 'unit:index.go:func Scan'}], relationships_removed: []},
  ]};
  assert.deepEqual(plain(walk.narrateChange(review, 0).map(text)), [
    'This change touches 2 files (+9 −3). We will go through it contracts first, then the implementation, tests and documentation.',
    'Now the implementation.',
    'Scan changed: 5 lines added, 2 removed.',
    'It is called from Use, which was not changed.',
    'Its test TestScan changed alongside it.',
    'It now calls walkTree and skip.',
    'It is no longer tested by TestOld.',
  ]);
  assert.deepEqual(plain(walk.narrateChange(review, 1).map(text)), ['Then the tests that came with the change.', 'TestScan is new (4 lines).']);
  assert.deepEqual(plain(walk.upNext({stops: review.changes}, 0)), {index: 1, node: plain(testNode), name: 'TestScan', path: 'index_test.go'});
  assert.equal(walk.upNext({stops: review.changes}, 1), null);
});

test('the brief is told most reliable first, and a comment says how old it is', () => {
  const fact = (kind, text, reliability, extra = {}) => ({kind, text, reliability, provenance: {path: 'work.go', start: 5}, ...extra});
  const brief = {facts: [
    fact('calls', 'func helper', 'current'),
    fact('test-states', 'work returns total; subtests: empty config', 'test', {node_id: 'unit:work_test.go:func TestWorkReturnsTotal', provenance: {path: 'work_test.go', start: 3}}),
    fact('commit', 'Return the real total', 'dated-history', {date: '2026-02-01'}),
    fact('commit', 'Add Work', 'dated-history', {date: '2026-01-01'}),
    fact('comment', 'Work sums the configured values.', 'comment-older-than-code', {date: '2026-01-01'}),
  ]};
  const told = walk.narrateBrief(brief);
  assert.deepEqual(plain(told.map(text)), [
    'Its test TestWorkReturnsTotal is named for what it checks: “work returns total; subtests: empty config”.',
    'These lines last changed on 2026-02-01: “Return the real total”.',
    'Before that: 2026-01-01 “Add Work”.',
    'The comment above it dates from 2026-01-01, before the code last changed, so it may be out of date: “Work sums the configured values.”',
  ]);
  assert.equal(told[0][1].node.unit_id, 'work_test.go:func TestWorkReturnsTotal');
  assert.match(text(walk.narrateBrief({facts: [fact('comment', 'Fresh.', 'comment-current', {date: '2026-03-01'})]})[0]), /no earlier than the code/);
  assert.deepEqual(plain(walk.narrateBrief(null)), []);
  // With the brief naming tests, the connections leave them out.
  const around = {node: {roles: []}, incoming: [], outgoing: [{kind: 'tested-by', to: 'unit:work_test.go:func TestWorkReturnsTotal'}], next: []};
  const described = walk.describe(around, brief).map(text);
  assert.equal(described.filter(line => line.includes('TestWorkReturnsTotal')).length, 1);
  assert.ok(walk.describe(around, null).map(text).includes('Tested directly by TestWorkReturnsTotal.'));
});
