const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const context = vm.createContext({URLSearchParams}); context.window = context;
vm.runInContext(fs.readFileSync(path.join(__dirname, '../ui/review.js'), 'utf8'), context);
const review = context.tlcrReview;
const plain = value => JSON.parse(JSON.stringify(value));

test('a typed revision is one commit or an explicit range', () => {
  assert.deepEqual(plain(review.parseRevision(' HEAD~2 ')), {commit: 'HEAD~2'});
  assert.deepEqual(plain(review.parseRevision('main..feature/x')), {base: 'main', head: 'feature/x'});
  for (const text of ['', 'a...b', '..b', 'a..', 'two words', 'x'.repeat(257)]) assert.equal(review.parseRevision(text), null, text);
  assert.equal(review.query({base: ':index'}), 'base=%3Aindex');
  assert.equal(review.query({}), '');
  assert.equal(review.query({commit: 'a b', ignored: 'x'}), 'commit=a+b');
});

test('listed changes filter by text and a typed revision comes last', () => {
  const list = {default_branch: 'main', working: [{kind: 'staged', title: 'Staged changes', detail: '', selection: {head: ':index'}}], commits: [{kind: 'commit', id: 'abc123', title: 'Fix parser', detail: 'someone', selection: {commit: 'abc123'}}], branches: null};
  assert.equal(review.choices(list, '').length, 2);
  const found = review.choices(list, 'parser');
  assert.deepEqual(plain(found.map(item => item.change.kind)), ['commit', 'revision']);
  assert.equal(found[0].section, 'Recent commits');
  assert.deepEqual(plain(review.choices(list, 'fix parser').map(item => item.change.kind)), ['commit']);
  assert.deepEqual(plain(review.choices(list, 'v1..v2')[0].change.selection), {base: 'v1', head: 'v2'});
});

test('diff rows preserve every source character and fall back from wrong tokens', () => {
  const hostile = '\tprintln("<script>alert(1)</script>")';
  const change = {before: {start: 10}, node: {start: 20}, hunks: [{lines: [
    {op: ' ', text: 'func main() {', before: 10, after: 20},
    {op: '-', text: '\tprintln(1)', before: 11},
    {op: '+', text: hostile, after: 21},
    {op: '+', text: '', after: 22},
    {op: ' ', text: '}', before: 12, after: 23},
  ]}]};
  const tokens = line => [{text: line.slice(0, 1), type: 'keyword'}, {text: line.slice(1), type: ''}];
  const before = ['func main() {', '\tprintln(1)', '}'].map(tokens);
  const after = ['func main() {', hostile, '', 'WRONG'].map(tokens);
  const rows = review.rows(change, before, after);
  assert.equal(rows[0].hunk, '@@ −10 +20 @@');
  const lines = rows.slice(1);
  assert.deepEqual(plain(lines.map(row => row.text)), change.hunks[0].lines.map(line => line.text));
  for (const row of lines) if (row.tokens) assert.equal(row.tokens.map(token => token.text).join(''), row.text);
  assert.equal(lines[2].tokens.length, 2);
  assert.equal(lines[4].tokens, null, 'tokens that do not reproduce the line are dropped');
  assert.deepEqual(plain(lines.map(row => [row.before, row.after])), [[10, 20], [11, 0], [0, 21], [0, 22], [12, 23]]);
  assert.ok(review.rows(change, null, null).slice(1).every(row => row.tokens === null));
  assert.deepEqual(plain(review.rows({node: {start: 1}, hunks: []}, null, null)), []);
});

test('stops group by cohort in order and unread search wraps', () => {
  const changes = [{id: 'a', cohort: '1 · types'}, {id: 'b', cohort: '2 · code'}, {id: 'c', cohort: '2 · code'}];
  assert.deepEqual(plain(review.groups(changes)).map(group => [group.cohort, group.items.map(item => item.index)]), [['1 · types', [0]], ['2 · code', [1, 2]]]);
  assert.equal(review.nextUnread(changes, new Set(['b']), 0), 2);
  assert.equal(review.nextUnread(changes, new Set(['b', 'c']), 2), 0);
  assert.equal(review.nextUnread(changes, new Set(['a', 'b', 'c']), 0), -1);
  assert.equal(review.nextUnread([], new Set(), 0), -1);
});

function fakeStorage(initial = {}) {
  const data = {...initial};
  return {data, getItem: key => key in data ? data[key] : null, setItem: (key, value) => { data[key] = String(value); }, removeItem: key => { delete data[key]; }};
}

test('marks and notes round-trip per repository and survive bad storage', () => {
  const store = fakeStorage();
  assert.equal(review.save(store, 'repo-a', new Set(['one', 'two']), {'unit:a': 'check the caller'}), true);
  const kept = review.load(store, 'repo-a');
  assert.deepEqual([...kept.read], ['one', 'two']);
  assert.deepEqual(plain(kept.notes), {'unit:a': 'check the caller'});
  assert.equal(review.load(store, 'repo-b').read.size, 0, 'another repository sees nothing');
  for (const bad of ['not json', '[]', '{"read":"x","notes":[1]}', '{"read":[1,null,"ok"],"notes":{"a":5,"b":""}}']) {
    const loaded = review.load(fakeStorage({'tlcr.review.r': bad}), 'r');
    assert.ok(loaded.read.size <= 1 && Object.keys(loaded.notes).length === 0, bad);
  }
  assert.equal(review.load(null, 'r').read.size, 0);
  assert.equal(review.save(null, 'r', new Set(), {}), false);
  assert.equal(review.save({setItem() { throw new Error('quota'); }}, 'r', new Set(), {}), false);
  assert.equal(review.load(fakeStorage({'tlcr.review.r': JSON.stringify({notes: {a: 'x'.repeat(5000)}})}), 'r').notes.a.length, 4000);
  review.clear(store, 'repo-a'); assert.equal(review.load(store, 'repo-a').read.size, 0); review.clear(null, 'r');
});

test('summary is a Markdown checklist with signals and notes', () => {
  const data = {base_label: 'commit abc', head_label: 'indexed working tree', files: 2, added: 5, removed: 1, changes: [
    {id: '1', cohort: '1 · types and contracts', status: 'modified', added: 4, removed: 1, node: {id: 'unit:a', name: 'type A', path: 'a.go', start: 3}, signals: [{detail: 'No test calls this unit directly'}]},
    {id: '2', cohort: '3 · tests', status: 'added', added: 1, removed: 0, node: {id: 'unit:t', name: 'func TestA', path: 'a_test.go', start: 9}, signals: []},
  ]};
  assert.equal(review.summary(data, new Set(['1']), {'unit:t': 'first line\nsecond line'}), [
    '# Review: commit abc → indexed working tree', '', '2 files · +5 −1 · 1 of 2 stops read', '',
    '## types and contracts', '- [x] `type A` · a.go:3 · modified · +4 −1', '  - No test calls this unit directly', '',
    '## tests', '- [ ] `func TestA` · a_test.go:9 · added · +1 −0', '  - Note: first line', '    second line', ''].join('\n'));
});
