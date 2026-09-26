/* Small, deliberately limited Markdown renderer. All content becomes text nodes;
   raw HTML, remote images and executable links are never interpreted. */
'use strict';
function renderMarkdown(text) {
  const root = document.createElement('div'); root.className = 'explanation';
  const inline = (parent, value) => {
    const pattern = /(`[^`\n]+`|\*\*[^*\n]+\*\*|\*[^*\n]+\*)/g;
    let offset = 0;
    for (const match of value.matchAll(pattern)) {
      parent.append(document.createTextNode(value.slice(offset, match.index)));
      const token = match[0], tag = token.startsWith('`') ? 'code' : token.startsWith('**') ? 'strong' : 'em';
      const element = document.createElement(tag), trim = tag === 'strong' ? 2 : 1;
      element.textContent = token.slice(trim, -trim); parent.append(element); offset = match.index + token.length;
    }
    parent.append(document.createTextNode(value.slice(offset)));
  };
  let pre = null, list = null;
  for (const line of String(text).split('\n')) {
    if (/^\s*```/.test(line)) {
      if (pre) pre = null;
      else { const container = document.createElement('pre'); pre = document.createElement('code'); container.append(pre); root.append(container); }
      list = null; continue;
    }
    if (pre) { pre.append(document.createTextNode(line + '\n')); continue; }
    if (!line.trim()) { list = null; continue; }
    const item = line.match(/^\s*(?:[-*+]\s+|\d+\.\s+)(.*)$/);
    if (item) {
      const tag = /^\s*\d+\./.test(line) ? 'OL' : 'UL';
      if (!list || list.tagName !== tag) { list = document.createElement(tag.toLowerCase()); root.append(list); }
      const li = document.createElement('li'); inline(li, item[1]); list.append(li); continue;
    }
    list = null;
    const heading = line.match(/^(#{1,6})\s+(.*)$/);
    const element = document.createElement(heading ? 'h' + heading[1].length : 'p'); inline(element, heading ? heading[2] : line); root.append(element);
  }
  return root;
}
