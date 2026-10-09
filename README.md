# tlcr

A local, read-only code reader with source-backed orientation, relationships, evidence and guided Tours. The CLI and browser UI ship in one Go binary and work without AI or an account. Terraform/HCL, Go, Python, shell scripts and Ansible playbooks/task lists are parsed structurally; frontend assets, other YAML and documents are readable as files. Optional AI enrichment uses an exact-payload preview and explicit approval.

## Quick start

Download the binary for your platform from the [Releases page](../../releases), build locally with the Go version pinned in `go.mod`, or build with Docker only (no local Go install — see [Building with Docker](#building-with-docker)):

```sh
go mod tidy
go build -o tlcr .
./tlcr /path/to/repo
```

This opens a browser bound to `127.0.0.1` on an available port. You can also use `./tlcr --no-browser .` and open the printed URL yourself. Close the process with Ctrl-C.

Release archives are named `tlcr-<platform>-<architecture>.tar.gz` (`.zip` on Windows). Extract the archive to get `tlcr` (`tlcr.exe` on Windows) and its `LICENSE`; no executable renaming is needed. Verify the archive against its accompanying SHA-256 checksum. Run `tlcr --version` to check which release you have.

The structural view needs no model and works without a config file. To enable explanations, copy `config.example.json` to `config.json` **beside the binary**, set `provider` to `openai` or `anthropic`, and set `model` to a model available to your API account. Export the matching `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`. You can instead pass `--config /path/to/config.json`. API access is billed by your provider; a ChatGPT or Claude subscription is not necessarily an API account.

`config.json` is intentionally gitignored; do not put API keys in it. The example leaves `model` blank so you choose an available model deliberately. The UI asks for confirmation each time before sending the highlighted block. No entire-repository upload or background generation occurs. The token limits are **estimates and safeguards**, not a guaranteed billing cap; provider-reported usage is shown after each call.

## Building with Docker

The `Makefile` and `Dockerfile` let you build and test this project with only Docker installed — no Go toolchain on your machine, and nothing written outside this repo.

```sh
make test    # go test ./...
make vet     # go vet ./...
make tidy    # go mod tidy (writes go.mod/go.sum back to the repo)
make fmt     # gofmt -l -w on the project's own *.go files
make build   # build ./dist/tlcr for your host OS/arch (auto-detected)
make cross   # build linux (amd64/arm64), macOS (arm64/amd64) and Windows binaries into ./dist
make shell   # interactive shell in the build container, for anything else
make clean   # remove ./dist and the module/build cache
```

How it stays isolated and portable:

- **One version, one place.** The Go version comes from `go.mod` (the `toolchain` line if present, else `go`), read by the `Makefile` with `awk`, so every target — local `make test`, `make build`, and CI's `go-version-file: go.mod` — always uses the same toolchain. Bump it in `go.mod` (or run e.g. `make test GO_VERSION=1.28` for a one-off check) and everything picks it up. The official Go images run with `GOTOOLCHAIN=local`, so this is what actually decides the patch version used, not just a minimum.
- **No host Go, no host pollution.** `make test`/`vet`/`tidy`/`fmt` run inside the official `golang` image via `docker run`, mounting the repo and running as your own user ID — files it writes (like `go.mod`/`go.sum` from `make tidy`) come back owned by you, not root.
- **Caches stay inside the repo.** Module and build caches live in `.dockerbuild/` (gitignored), not in `~/go` or a Docker volume shared across other projects — `make clean` removes it entirely.
- **Release builds need no running container.** `make build`/`make cross` use `docker buildx build --output` to compile inside a throwaway build stage and copy the resulting binaries straight into `./dist`, matching the same GOOS/GOARCH matrix as the `Test and build` GitHub Actions workflow, with `-trimpath` and stripped symbols for reproducible, smaller binaries.

This covers building and testing only. Actually running `tlcr` still needs a real terminal/browser on your host (it binds to `127.0.0.1` and opens a browser window), so use a locally built or downloaded binary for day-to-day use, not a container.

## Continuous integration

`.github/workflows/ci.yml` runs on every push, pull request, and weekly on a schedule (to catch newly-disclosed CVEs even when nothing has changed). All third-party actions are pinned by commit SHA, not a mutable tag.

| Job | What it checks |
| --- | --- |
| `lint` | `gofmt`, `go vet`, and `golangci-lint` (config in `.golangci.yml`), including its bundled `gosec` static-security linter |
| `mod-tidy` | `go mod tidy -diff` and `go mod verify` — `go.mod`/`go.sum` must already be tidy |
| `govulncheck` | known vulnerabilities reachable from this code, stdlib included |
| `actionlint` / `hadolint` | lint the workflow YAML itself and the `Dockerfile` |
| `test` | `go test -race` on Linux, macOS *and* Windows (this project has OS-specific logic, e.g. `openBrowser`'s per-GOOS switch), with a coverage floor and per-OS coverage artifacts |
| `build` | cross-compiles and checksums release binaries for linux/amd64, linux/arm64, darwin/arm64, darwin/amd64 and windows/amd64, gated on lint/mod-tidy/govulncheck/test passing first |

A handful of `gosec`/`govet` findings are suppressed inline with a native `// #nosec Gxxx -- reason` comment where the flagged code is deliberate — e.g. reading files from a repository this tool was explicitly pointed at is not an untrusted-path vulnerability.

This repo is private without GitHub Advanced Security, so CodeQL, SARIF-based security scanning, and full Dependency Review — all of which upload to the Security tab or use GHAS-gated APIs — aren't available here; they fail (e.g. "Resource not accessible by integration", or dependency-review failing in seconds) regardless of the workflow's `permissions:` block, since it's a plan limitation, not a configuration bug. Those three jobs are commented out at the bottom of `ci.yml` rather than deleted — uncomment them if this repo goes public or GHAS gets enabled. `golangci-lint`'s `gosec` linter and `govulncheck` cover the same ground in the meantime without needing GHAS.

## Releasing

`.github/workflows/release.yml` is manual: **Actions → Release → Run workflow** on `main`. It:

1. Runs `go test`/`go vet` as a sanity gate.
2. Computes the next version tag as `vYYYY.MM.N` — a build counter that resets each month (e.g. `v2026.09.1`, then `v2026.09.2` for a same-month re-release) — and pushes it.
3. Cross-compiles linux/amd64, linux/arm64, darwin/arm64, darwin/amd64 and windows/amd64 with that version embedded (`tlcr --version` reports it) and `-trimpath`/stripped symbols, and checksums each binary.
4. Publishes a GitHub Release for the tag with all executable archives and checksums attached, and auto-generated release notes from commits since the last release.

There's no semantic versioning here — the date-based tag just marks when a build was cut, since this project doesn't track API/compatibility guarantees between releases yet.

## Current capabilities

- Navigate Terraform blocks and local `module` source paths across stacks and reusable modules.
- Navigate Go functions, methods and types; view HTML, JS, TS and CSS files as whole-file units.
- Generate explanations only for selected source blocks. A local module's block names are included as a compact context hint.
- Cache explanations in the user cache directory, keyed by source, context, provider, model and prompt version. Reopening unchanged code uses no model tokens.
- Exclude `.terraform`, `.git`, dependency directories, state/plan files, `.tfvars`, config and `.env` from indexing, on top of respecting the repository's own `.gitignore` files (root and nested, including negation) — so build artifacts, `node_modules`, compiled binaries, and anything else a project already ignores don't clutter the browser. Source files can still contain secrets: review highlighted code before approving any send.

## Deliberate MVP limits

This is a source reader, not a Terraform plan, IAM policy evaluator, complete call graph or security scanner. It does not run Terraform, edit the target repository, or evaluate variables. For unsupported languages, structural navigation is at file level. `.gitignore` matching covers the common syntax (negation, anchoring, directory-only patterns, `**`) via a small dependency-free matcher, not a byte-for-byte reimplementation of git's own wildmatch — very unusual patterns may match slightly differently than `git check-ignore` would. As in git itself, a pattern inside an already-ignored directory's own `.gitignore` cannot re-include files there (`vendor/` at the root always wins, however `vendor/keep/.gitignore` is written). Files a `.gitignore` doesn't cover can still contain secrets: review highlighted code before approving any send.

The binary uses standard Go networking to call the configured provider directly over HTTPS. It does not bundle model weights. The sidecar config is JSON. Local browser assets are embedded at build time.

## Dogfood checks

1. Run it on this repo: open `index.go` and follow parsing functions, then `server.go` and model calls.
2. Run it on a Terraform repository: open a root module's `main.tf`, inspect its resource blocks, and follow a local `module` source link to the module it points at.
3. Explain one small block, refresh without changing it (cache hit), then edit the block and explain again (new call).

## Product rename and compatibility

The canonical product and executable are now **tlcr**. No legacy CLI alias is installed. The GitHub repository is now `tathagata/tlcr`; GitHub redirects the old URL, so existing clones keep working. The Go module path remains `github.com/tathagata/coderead`; renaming it is deferred.

Configuration is unchanged: `config.json` beside the executable, or the explicit `--config` path. Provider API-key environment variables are unchanged. There are no product-specific environment variables or browser storage keys to migrate.

New explanations are cached under the operating system's user-cache directory at `tlcr/<repository-hash>/`. Existing entries under the legacy `coderead/<repository-hash>/` directory remain readable as a fallback, with the same source/context/provider/model/prompt key. New entries take precedence. Legacy entries are neither modified nor deleted. Both `.tlcr/` and the legacy `.coderead/` repository-local directories remain excluded from indexing. No target source is changed by this migration.

See [the implementation roadmap](docs/ROADMAP.md) for the dependency-ordered path to deterministic comprehension, evidence and Tours before optional AI enrichment.

### Read without AI

Opening a repository now starts with a deterministic orientation: ranked entry points, source-backed roles, callers, local dependencies, and direct test relationships. Read Next links every recommendation to its source provenance. Git history and exact ADR/changelog references are local evidence; unavailable Git does not prevent browsing.

Choose Architecture, Execution Flow, Data & State, Testing Strategy, or Recent Changes from the Tour menu. Use `n`/`p` for next/previous, `[`/`]` for reading history, `/` to find files or symbols, and `?` for the complete keyboard map. Tours can be paused and resumed in the same page. Refresh explicitly rebuilds the snapshot after edits; navigation does not rescan the repository.

The same core is available without the browser:

```sh
tlcr tour --kind architecture ./repository
tlcr tour --kind testing --json ./repository
tlcr review --base HEAD ./repository
tlcr review --base main --json ./repository
```

`review` compares a local commit with the indexed working-tree snapshot, including staged and untracked permitted source. It makes no GitHub, model, or network requests. Change Tour is also available in the browser, with a local base selector and base/working-tree source panes. JSON includes before/after source, added/removed relationships, related callers/tests, and an ordered tour. Removed units retain their base source. Renamed symbols are conservatively reported as added/removed, and text outside parsed units gets a file-context stop. Standard Go generated files can be collapsed when both versions contain the generated-code marker. GitHub review comments are not integrated yet.

Go and Terraform have structural units and local relationships. Python has one unit per top-level `def`/`async def`/`class` (decorators included, methods not split out), found by indentation rather than a full parse. Shell scripts (`.sh`, `.bash`, or an extension-less file with an `sh`/`bash` shebang) have one unit per function; a definition whose opening brace is on the next line is not recognized. Ansible playbooks have one unit per play and task lists one per task, classified from the YAML structure rather than the path, with nothing templated or executed. Other YAML (including Kubernetes manifests), frontend and document files currently have whole-file fallback units, as does any file whose structure cannot be read. The Go graph resolves indexed local packages without running builds or downloading modules; external calls, dynamic interface dispatch and unresolved references are omitted. Role ranking and Tour ordering are explicitly heuristic, not proof of runtime behavior or correctness.

Source highlighting is fully embedded and works offline for Go, HCL, Python, shell, YAML, JS/TS, JSX/TSX, HTML and CSS. Unsupported kinds remain plain text. Tokenization runs in a local worker with a timeout, preserves multiline tokens, and never inserts source as HTML. Files longer than 2,000 lines have explicit earlier/later navigation. See [vendored dependency details](docs/THIRD_PARTY.md).

### Explicit AI enrichment

The optional AI action first shows the exact prompt, provider/model and estimated input size. You can choose whether to include bounded local evidence. Approval is bound to the payload digest; source or evidence changes require a new preview. AI output stays separate from source facts. Model output is rendered with a small text-node Markdown renderer; raw HTML, images and executable links are not enabled.

### Analysis limits

Indexing is bounded to 4,096 recognized files / 32 MiB, 256 KiB per file, 50,000 visited entries and 64 directory levels. Symlinks, nonregular files, ignored paths, known private state and Ansible Vault ciphertext (wholly encrypted files, and YAML files with inline `!vault` values) are omitted. Changes to exclusion rules or module identity invalidate the snapshot. A local change review allows 128 historical blob reads, 4 MiB of returned changed source, 2,000 change entries and a 15-second Git-operation deadline. Large comparisons fail clearly and can be narrowed by choosing a smaller root or closer base. Tours show at most 32 change stops; all detected changes remain in the review result.

Go regression tests run with `make test`; pure-JavaScript highlighting tests run with `make test-ui` (Docker; no npm installation needed). Runtime exploration remains read-only toward the target repository. Explanation cache writes use private temporary files and atomic replacement.

## License

[MIT](LICENSE). Vendored third-party code keeps its own license; see [docs/THIRD_PARTY.md](docs/THIRD_PARTY.md).
