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
