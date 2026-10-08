'use strict';
self.Prism = {manual: true, disableWorkerMessageHandler: true};
importScripts('/prism-core.js', '/prism-clike.js', '/prism-go.js', '/prism-hcl.js', '/prism-markup.js', '/prism-css.js', '/prism-javascript.js', '/prism-typescript.js', '/prism-jsx.js', '/prism-tsx.js', '/prism-python.js', '/prism-bash.js', '/prism-yaml.js');
self.onmessage = ({data}) => {
  try {
    const {source, language} = data;
    if (typeof source !== 'string' || source.length > 262144 || !Object.hasOwn(Prism.languages, language)) throw new Error('Unsupported input');
    const lines = [[]]; let count = 0;
    function flatten(value, type = '') {
      if (typeof value === 'string') {
        const parts = value.split('\n');
        parts.forEach((text, i) => {
          if (i) lines.push([]);
          if (text) lines[lines.length - 1].push({text, type});
          if (++count > 300000 || lines.length > 20000) throw new Error('Token limit');
        });
      } else if (Array.isArray(value)) value.forEach(token => flatten(token, type));
      else flatten(value.content, value.type);
    }
    flatten(Prism.tokenize(source, Prism.languages[language]));
    self.postMessage({lines});
  } catch { self.postMessage({lines: null}); }
};
