# Vendored syntax highlighting

PrismJS 1.30.0 (MIT), downloaded from the npm registry and verified against the published SHA-512 integrity value. Upstream: https://github.com/PrismJS/prism. Tokenization API: https://prismjs.com/extending.

Only the core and explicit grammars are embedded; no CDN, autoloader, DOM-highlighting plugin or runtime package fetch is used. A dedicated local worker tokenizes entire files to retain multiline state. Our renderer uses text nodes, never HTML from the tokenizer. Unsupported languages remain plain text; worker timeout falls back to plain text.

Prism was selected for its separate grammars, token-stream API and MIT license. Go, HCL, Python, Bash, YAML, JS/TS, JSX/TSX, HTML and CSS are included.

Total JavaScript: 33437 bytes, uncompressed on disk. License: `ui/PRISM-LICENSE.txt`.

| File | Bytes | SHA-256 |
|---|---:|---|
| prism-core.js | 7461 | 6caad316dd991f24f8004e0b9c19c055cb5829ff65e973fbee406f96d81b8e7e |
| prism-clike.js | 708 | c76ba4e240932bdc75546be30e550f5ba5e13815ff71511c76e9e27ac3072444 |
| prism-go.js | 970 | 1225b4afb593126d4082da5fd2b131aede39831c2b2a62d6b07ea025acd2bf3f |
| prism-hcl.js | 1392 | 6c8bc9ea13f7ad08648eb2ffdda99d5ed674844220b2b4757aeb19d06fc78b18 |
| prism-markup.js | 2850 | 879fc9d256c352d980e053857fa707330853b8bfb67ce284ea661a24dec5756e |
| prism-css.js | 1232 | 8c9760dba7f26ea842016919544dd9b73a78a36d5b07a1e9842c333ed18ab6ae |
| prism-javascript.js | 4611 | 0345ea83e12b7b974e953c79a64dea35a40308309449db70b82020fb688ac321 |
| prism-typescript.js | 1294 | 852f5513bb9ca9db247f86ecfce74acc91c541749d34929157240518fef8152a |
| prism-jsx.js | 2388 | 0c8b80e4d98f6813ef95fd0e7ae2862cc0804ec305e0ad1f99c0a4bb7c28f865 |
| prism-tsx.js | 305 | 752c15ed4ff1d03e042b407b332892e1097d5f5e348861e2e26db20d71b349bf |
| prism-python.js | 2113 | ed4385685bcf2d4935c8dbbab4bde16603da1329e092d2bf36c3dadd67e9a85c |
| prism-bash.js | 6143 | 6260814110e5182f2956e3bd257429548d9dbf2a9b66a63719b26cf9fac966a7 |
| prism-yaml.js | 1970 | 719c8e8b8c344dc9de510c729f65ba840b1502a0a8e7e25e2ad19ee715f65c02 |

# YAML parsing

`go.yaml.in/yaml/v3` (MIT and Apache-2.0), the YAML organization's maintained continuation of the archived `gopkg.in/yaml.v3`, pinned in `go.mod`/`go.sum`. It is the only YAML library; Ansible parsing uses it and Kubernetes support must reuse it. Documents are decoded to nodes for structure and line numbers only: aliases are not expanded and no values are evaluated.
