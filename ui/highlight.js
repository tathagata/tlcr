'use strict';
// Only explicit indexed kinds select grammars. No content detection or remote assets.
window.tlcrHighlight = (() => {
  let worker = null, pending = null;
  const language = (kind, path) => kind === 'go' ? 'go' : kind === 'terraform' ? 'hcl' : kind === 'python' ? 'python' : kind === 'shell' ? 'bash' : kind === 'ansible' || kind === 'yaml' ? 'yaml' : kind === 'frontend' ? ({js:'javascript',jsx:'jsx',ts:'typescript',tsx:'tsx',html:'markup',css:'css'}[path.split('.').pop().toLowerCase()] || '') : '';
  function cancel() { if (pending) pending(null); }
  function tokenize(source, kind, path) {
    cancel(); const grammar = language(kind, path);
    if (!grammar || typeof Worker === 'undefined') return Promise.resolve(null);
    return new Promise(resolve => {
      let timer;
      const finish = lines => { clearTimeout(timer); worker?.terminate(); worker = null; pending = null; resolve(lines); };
      pending = finish;
      try {
        worker = new Worker('/highlight-worker.js');
        worker.onmessage = event => finish(event.data.lines);
        worker.onerror = () => finish(null);
        timer = setTimeout(() => finish(null), 1500);
        worker.postMessage({source, language: grammar});
      } catch { finish(null); }
    });
  }
  return {tokenize, cancel};
})();
