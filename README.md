# CodeRead

An on-demand, read-only code reader: source on the left, generated explanation on the right. The CLI and browser UI ship in one Go binary. Terraform/HCL and Go are parsed structurally; frontend assets are viewable as files. No code goes to an AI provider until you select a block and approve the request.

## Quick start

Download the binary for your platform from the [Releases page](../../releases), build locally with the Go version pinned in `go.mod`, or build with Docker only (no local Go install — see [Building with Docker](#building-with-docker)):

```sh
go mod tidy
go build -o coderead .
./coderead /path/to/repo
```

This opens a browser bound to `127.0.0.1` on an available port. You can also use `./coderead --no-browser .` and open the printed URL yourself. Close the process with Ctrl-C.

Downloaded macOS/Linux binaries may need `chmod +x` after download. Run `coderead --version` to check which release you have.

The structural view needs no model and works without a config file. To enable explanations, copy `config.example.json` to `config.json` **beside the binary**, set `provider` to `openai` or `anthropic`, and set `model` to a model available to your API account. Export the matching `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`. You can instead pass `--config /path/to/config.json`. API access is billed by your provider; a ChatGPT or Claude subscription is not necessarily an API account.

`config.json` is intentionally gitignored; do not put API keys in it. The example leaves `model` blank so you choose an available model deliberately. The UI asks for confirmation each time before sending the highlighted block. No entire-repository upload or background generation occurs. The token limits are **estimates and safeguards**, not a guaranteed billing cap; provider-reported usage is shown after each call.

## Building with Docker

The `Makefile` and `Dockerfile` let you build and test this project with only Docker installed — no Go toolchain on your machine, and nothing written outside this repo.

```sh
make test    # go test ./...
make vet     # go vet ./...
make tidy    # go mod tidy (writes go.mod/go.sum back to the repo)
make fmt     # gofmt -l -w on the project's own *.go files
make build   # build ./dist/coderead for your host OS/arch (auto-detected)
make cross   # build linux (amd64/arm64), macOS (arm64/amd64) and Windows binaries into ./dist
make shell   # interactive shell in the build container, for anything else
make clean   # remove ./dist and the module/build cache
```

How it stays isolated and portable:

- **One version, one place.** The Go version comes from `go.mod` (the `toolchain` line if present, else `go`), read by the `Makefile` with `awk`, so every target — local `make test`, `make build`, and CI's `go-version-file: go.mod` — always uses the same toolchain. Bump it in `go.mod` (or run e.g. `make test GO_VERSION=1.28` for a one-off check) and everything picks it up. The official Go images run with `GOTOOLCHAIN=local`, so this is what actually decides the patch version used, not just a minimum.
- **No host Go, no host pollution.** `make test`/`vet`/`tidy`/`fmt` run inside the official `golang` image via `docker run`, mounting the repo and running as your own user ID — files it writes (like `go.mod`/`go.sum` from `make tidy`) come back owned by you, not root.
- **Caches stay inside the repo.** Module and build caches live in `.dockerbuild/` (gitignored), not in `~/go` or a Docker volume shared across other projects — `make clean` removes it entirely.
- **Release builds need no running container.** `make build`/`make cross` use `docker buildx build --output` to compile inside a throwaway build stage and copy the resulting binaries straight into `./dist`, matching the same GOOS/GOARCH matrix as the `Test and build` GitHub Actions workflow, with `-trimpath` and stripped symbols for reproducible, smaller binaries.

This covers building and testing only. Actually running `coderead` still needs a real terminal/browser on your host (it binds to `127.0.0.1` and opens a browser window), so use a locally built or downloaded binary for day-to-day use, not a container.

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
3. Cross-compiles linux/amd64, linux/arm64, darwin/arm64, darwin/amd64 and windows/amd64 with that version embedded (`coderead --version` reports it) and `-trimpath`/stripped symbols, and checksums each binary.
4. Publishes a GitHub Release for the tag with all binaries and checksums attached, and auto-generated release notes from commits since the last release.

There's no semantic versioning here — the date-based tag just marks when a build was cut, since this project doesn't track API/compatibility guarantees between releases yet.

## Current capabilities

- Navigate Terraform blocks and local `module` source paths across stacks and reusable modules.
- Navigate Go functions, methods and types; view HTML, JS, TS and CSS files as whole-file units.
- Generate explanations only for selected source blocks. A local module's block names are included as a compact context hint.
- Cache explanations in the user cache directory, keyed by source, context, provider, model and prompt version. Reopening unchanged code uses no model tokens.
- Exclude `.terraform`, `.git`, dependency directories, state/plan files, `.tfvars`, config and `.env` from indexing. Source files can still contain secrets: review highlighted code before approving any send.

## Deliberate MVP limits

This is a source reader, not a Terraform plan, IAM policy evaluator, complete call graph or security scanner. It does not run Terraform, edit the target repository, or evaluate variables. For unsupported languages, structural navigation is at file level. It does not currently integrate `.gitignore` patterns beyond the explicit exclusions above; do not point it at a repository containing secrets in otherwise included source files unless you will review every snippet before sending.

The binary uses standard Go networking to call the configured provider directly over HTTPS. It does not bundle model weights. The sidecar config is JSON to avoid an additional YAML dependency. Local browser assets are embedded at build time.

## Dogfood checks

1. Run it on this repo: open `index.go` and follow parsing functions, then `server.go` and model calls.
2. Run it on `tathagata/aws`: open `live/shared/prod/main.tf`, inspect the OIDC and IAM blocks, and follow the local module link from `live/blog/dev/main.tf`.
3. Explain one small block, refresh without changing it (cache hit), then edit the block and explain again (new call).
