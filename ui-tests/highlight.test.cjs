const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const { performance } = require('node:perf_hooks');
const root = path.join(__dirname, '../ui');
function tokenize(source, language) {
  let result;
  const context = vm.createContext({}); context.self = context;
  context.importScripts = (...names) => names.forEach(name => vm.runInContext(fs.readFileSync(path.join(root, name), 'utf8'), context, {timeout: 1500}));
  context.postMessage = data => { result = data.lines; };
  vm.runInContext(fs.readFileSync(path.join(root, 'highlight-worker.js'), 'utf8'), context);
  context.input = {source, language};
  vm.runInContext('onmessage({data: input})', context, {timeout: 1500});
  return result;
}
for (const [language, source] of [
  ['go','package main\n/* first\nsecond */\nfunc main() { println("<script>alert(1)</script>") }\n'],
  ['hcl','resource "test" "sample" {\n  value = <<EOF\nfirst\nsecond\nEOF\n}\n'],
  ['javascript','const text = `<img onerror="evil()">`;\n'],
  ['typescript','interface Item { name: string }\n'],
  ['markup','<script>alert("not executed")</script>\n'],
  ['css','/* comment */ body { color: red; }\n'],
  ['jsx','const component = <p>{value}</p>;'],
  ['tsx','const component: Element = <p>{value}</p>;'],
  ['python','@decorated\ndef main():\n    """first\n    second"""\n    return f"<b>{value}</b>"\n'],
  ['bash','#!/bin/bash\ndeploy() {\n  cat <<EOF\n<script>${HOME}</script>\nEOF\n}\n'],
  ['yaml','- name: Install\n  apt:\n    name: "{{ item }}"\n  notes: |\n    first\n    second\n']
]) test(`${language}: preserves every source character and line`, () => {
  const lines = tokenize(source, language);
  assert.ok(lines); assert.equal(lines.map(row => row.map(token => token.text).join('')).join('\n'), source);
  assert.ok(lines.some(row => row.some(token => token.type)));
  if (language === 'go') assert.equal(lines[2][0].type, 'comment');
});
test('unsupported grammar and oversized input safely fall back', () => {
  assert.equal(tokenize('plain text','unknown'), null);
  assert.equal(tokenize('a'.repeat(262145),'go'), null);
});
test('near-limit source is bounded and reconstructs exactly', t => {
  const source = 'func Work() { println("hello") }\n'.repeat(8000).slice(0, 262144);
  const start = performance.now(); const lines = tokenize(source, 'go');
  t.diagnostic(`Tokenized ${source.length} characters in ${(performance.now()-start).toFixed(1)} ms`);
  assert.ok(lines); assert.equal(lines.map(row => row.map(token => token.text).join('')).join('\n'), source);
});
