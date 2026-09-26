# tlcr implementation roadmap

Snapshot: 2026-09-16. Covers all 23 open issues (#3–#25) in tathagata/coderead. This is a dependency order, not an assertion that the features are implemented. Work stays read-only toward repositories being analyzed. AI is an optional consumer of the same evidence available to the engineer.

## Topological order

A valid linear order is:

**25 → 21 → 22 → 3 → 4 → 7 → 10 → 11 → 17 → 18 → 23 → 24 → 9 → 20 → 12 → 13 → 16 → 15 → 14 → 19 → 5 → 6 → 8**

Dependencies below distinguish implementation prerequisites from chosen sequencing. Independent issues can move earlier without violating the graph, but optional AI work must not displace the deterministic comprehension milestone.

| Order | Issue | Prerequisites | Deliverable / completion evidence |
|---|---|---|---|
| 1 | [#25 Product rename](https://github.com/tathagata/coderead/issues/25) | None; chosen first to stabilize public identity | Canonical executable/UI/releases, documented legacy state behavior, stale-name audit and multi-platform build checks. Repository/module rename explicitly deferred. |
| 2 | [#21 Reusable core](https://github.com/tathagata/coderead/issues/21) | #25 sequencing | HTTP-independent repository and explanation services; typed errors; unchanged consent/budget/cache behavior tested at adapter boundaries. |
| 3 | [#22 Comprehension graph](https://github.com/tathagata/coderead/issues/22) | #21 | Start with current Go/Terraform data. Stable unique IDs, provenance, containment/module relationships, neighbor queries and reproducible ranking. No model/network. |
| 4 | [#3 Structural intelligence](https://github.com/tathagata/coderead/issues/3) | #21, #22 contract | Content-aware parser registry with explicit precedence, normalized relationships and Go cross-file evidence. Optional tools degrade gracefully. |
| 5 | [#4 Evidence providers](https://github.com/tathagata/coderead/issues/4) | #21, #22 contract | Structured, bounded, provenance-bearing evidence usable by UI and separately approved AI context. Provider errors do not break browsing. |
| 6 | [#7 Git identity/cache](https://github.com/tathagata/coderead/issues/7) | #21 | Introduce bounded, read-only Git helper. Preserve working-tree changes, prompt/context/model identity and non-Git fallback. See correction below. |
| 7 | [#10 Git history evidence](https://github.com/tathagata/coderead/issues/10) | #4, shared Git helper from #7 | Local recent commits/churn with source provenance; non-Git/shallow/untracked fixtures; expensive blame loads lazily. |
| 8 | [#11 ADR/changelog evidence](https://github.com/tathagata/coderead/issues/11) | #4 | Conservative exact references, navigable excerpts and exclusion checks. Documents remain untrusted evidence. |
| 9 | [#17 Read Next](https://github.com/tathagata/coderead/issues/17) | #22, #3; #4 for evidence destinations | Ranked source destinations with reasons, provenance, reading history and unit-level navigation. |
| 10 | [#18 Orientation](https://github.com/tathagata/coderead/issues/18) | #3, #4, #17; #10 for history | Repository/file/unit role, boundaries, callers, dependencies, tests, history and next stops without AI. |
| 11 | [#23 Tours](https://github.com/tathagata/coderead/issues/23) | #22, #17; #10 for Recent Changes | Stable Architecture, Execution, Data/State, Testing and Recent Changes paths when supported by evidence; reasons and exact source per stop; resume/previous/next. Change Tour follows in #20. |
| 12 | [#24 Keyboard-first UX](https://github.com/tathagata/coderead/issues/24) | #17, #23 | Semantic commands, scoped shortcuts, accurate help, mouse-free 10-stop tour and consent-preserving AI action. |
| 13 | [#9 Syntax highlighting](https://github.com/tathagata/coderead/issues/9) | None; here by priority | Vendored local assets, safe token rendering and correct multiline highlighting without breaking source ranges. |
| 14 | [#20 Change comprehension](https://github.com/tathagata/coderead/issues/20) | #3, #4, #17, #22, #23, shared Git helper | Local changed-unit/affected-neighborhood Change Tour first; optional read-only GitHub evidence later with explicit network boundary. |
| 15 | [#12 Python](https://github.com/tathagata/coderead/issues/12) | #3 | Trustworthy top-level units/decorators and fallback; no invented call graph. Prefer existing parser intelligence to fragile compiler reimplementation. |
| 16 | [#13 Shell](https://github.com/tathagata/coderead/issues/13) | #3 | Function ranges and shebang detection with quoting/heredoc fixtures; never execute scripts. |
| 17 | [#16 Kubernetes](https://github.com/tathagata/coderead/issues/16) | #3 | Shared YAML parsing/classification, multi-document provenance and safe template fallback; no cluster access or template execution. |
| 18 | [#15 Ansible](https://github.com/tathagata/coderead/issues/15) | #3; #16 chosen to establish shared YAML first | Plays/tasks with source ranges; Kubernetes wins unambiguous classification; encrypted Vault content excluded. No playbook execution. |
| 19 | [#14 C/C++](https://github.com/tathagata/coderead/issues/14) | #3 | Optional bounded ctags integration; fixed arguments; configuration isolation; whole-file fallback without ctags. |
| 20 | [#19 Findings](https://github.com/tathagata/coderead/issues/19) | #3 and ≥1 of #12–#16; #4 for evidence integration | One language end-to-end, no auto-fixes; explicit opt-in before external tool execution; unavailable/failed analysis distinguished from clean results. |
| 21 | [#5 Local model](https://github.com/tathagata/coderead/issues/5) | #21; deterministic milestone is a priority gate | Explicit endpoint boundary, fake-server tests, no automatic connection/discovery; no effect on offline comprehension. |
| 22 | [#6 Session consent](https://github.com/tathagata/coderead/issues/6) | #21; deterministic milestone is a priority gate | Launch-only consent policy, persistent indicator and unchanged default rejection; limits/origin checks remain enforced. |
| 23 | [#8 AI Markdown](https://github.com/tathagata/coderead/issues/8) | None; optional AI priority gate | Safe formatted interpretation, no raw HTML execution/remote assets and unchanged CSP. |

## Resolve apparent cycles and stale assumptions

- **#22 ↔ #3:** define the graph vocabulary using today's Go/Terraform index first; the parser registry then emits that vocabulary. Do not require all parsers to exist before the graph, or all graph features before a parser.
- **#21 ↔ #4:** extract current behavior and service boundaries first, then introduce structured evidence. Avoid making explanation the architectural center.
- **#23 ↔ #20:** Tours owns the shared engine; #20 adds Change Tour. The Tours issue need not wait for its own extension.
- **#6 → #18 is obsolete:** orientation is deterministic and must never need consent or a model. Session auto-approval only affects an optional AI action.
- **#7 → #10:** the real dependency is a safe local Git reader. Existing content/prompt hashing already allows reuse after returning to identical content across branches. A committed blob SHA alone is unsafe for dirty working trees. Preserve prompt, evidence, model and working-tree invalidation; do not postpone history on an unnecessary cache redesign.
- **#15 ↔ #16:** share YAML parsing and deterministic classification; Kubernetes first is a sequencing choice, not a mutual dependency.
- **#19:** absence of a tool means “not analyzed,” never “no problems.” Repository tool configuration can load code/plugins, so analysis must not silently execute tools on opening a repo.

## Gates on every implementation slice

1. **Read-only and confined:** analyze only indexed, permitted sources; test excluded files, nested ignores, symlink escapes, changed paths and private state. No source writes, Git index mutations, build hooks, autofixes or background tool execution.
2. **Offline by construction:** run structural integration tests with networking disabled, no provider credentials and unavailable optional tools. Source, graph, evidence, ranking and Tours remain useful.
3. **Traceable and conservative:** each fact/edge has provider + source/range or commit/document provenance. Ambiguous relationships are omitted or explicitly uncertain. Ranking is labeled heuristic; AI text never enters deterministic facts.
4. **Reliable under change:** stable unique IDs, deterministic tie-breaking, bounded reads/timeouts/subprocess output, cancellation, stale-index handling and graceful provider failure. Navigation queries must not rescan the entire repository.
5. **Explicit AI boundary:** preview the actual bounded source/evidence payload and destination; bind approval to that payload so edits cannot silently change what is sent. Preserve server-side consent, origin checks, budgets and cache isolation. External services remain optional.
6. **Safe rendering/distribution:** untrusted code/docs/model text are escaped/sanitized, local embedded assets only, restrictive CSP, dependency/security checks and tested release archives.
7. **Useful to a human:** dogfood on tlcr; opening a repository should lead naturally through entry points, relationships, tests and evidence. Record a repeatable Tour transcript, including why each stop appears, without an AI call.

## First implementation slice

Start #25 with canonical identity, archives containing `tlcr`/`tlcr.exe`, and read-legacy/write-new cache behavior. Keep the repository/module URL unchanged. Preserve the pre-existing uncommitted ignore work.

Before #22 expands index responsibilities, resolve the observed baseline risks with targeted regressions: malformed ignore character classes can panic; method IDs omit receiver type; whole-file fallback differs between scan and read; tree requests rescan synchronously. These are foundation work, not additional AI features.

The first product milestone is #21–#24 plus evidence: a useful offline orientation and repeatable guided reading path through tlcr itself. Expanded languages, change review and findings deepen that experience. Optional AI then interprets a rich, explicitly chosen evidence bundle rather than merely paraphrasing a selected block.

## Current implementation checkpoint

The rename and local-first foundation are implemented locally: reusable core, parser registry, normalized Go/HCL graph, Git-aware explanation identity, bounded Git/document evidence, orientation, Read Next, Tours, keyboard navigation, exact AI payload previews and safe text-node output rendering. The browser and `tlcr tour` use the same core. Existing uncommitted work was preserved.

Syntax highlighting (#9) is implemented with vendored Prism 1.30.0 (23,211 JavaScript bytes, MIT), same-origin worker tokenization, text-node rendering, plain-text fallback and bounded line windows. Go/HCL/plain-text views and a 256 KiB file were exercised in the browser; ten source-preservation/tokenization tests pass. No console errors or warnings were observed.

Local Change Tour (#20 phase 1) is implemented in `tlcr review`, `/api/review`, and the browser. It compares a specified available local commit with the indexed working tree, reports changed parser units and resolved relationship deltas, surfaces known callers/tests, retains removed source, and provides deterministic cohorts and ordered stops. Symbol renames appear conservatively as removed/added. Only standard Go generated markers are collapsed. GitHub evidence remains a design document, not a network integration.

Validation: offline race tests, configured Go lint (zero issues), vet, module verification/tidy, native no-provider API smoke checks, five target cross-compiles, and current vulnerability checks. Browser checks verified source highlighting, safe literal hostile text, Change Tour source panes and keyboard advancement. Native runtime tests on Windows/Linux/macOS CI and hosted release execution remain outstanding. No issues have been closed, commits created or changes pushed.

Remaining feature work: expanded Python/Shell/Kubernetes/Ansible/C++ parsing (#12–16), explicit optional local linter evidence (#19), local model providers (#5), launch-only autoapproval (#6), and full acceptance audits of the broad graph/evidence/orientation issues. Optional GitHub review evidence is a separate future step with an explicit network boundary. Additional graph depth (e.g. callback registrations and ambiguous build variants) must stay conservative. Do not count issue closure from this checkpoint alone.
