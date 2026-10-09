# Security Policy

tlcr is a personal, single-maintainer project. There's no dedicated security team and no formal SLA, but reports are taken seriously and triaged promptly.

## Supported versions

Only the latest release and the tip of `main` are supported. Security fixes land on `main` and ship in the next date-tagged release; download it from the Releases page, or build a fresh binary, to pick them up.

## Reporting a vulnerability

Please **do not open a public GitHub issue** for a suspected vulnerability.

Use GitHub's private vulnerability reporting for this repo: go to the **Security** tab → **Report a vulnerability**, and include a description and, if possible, steps to reproduce.

You should get an acknowledgment within a few days. Fixes ship as soon as practical given this is maintained in spare time; there's no guaranteed patch window.

## Scope

In scope — bugs in tlcr itself that would let it do something it isn't supposed to:

- Path traversal or symlink escapes that let it read files outside the repository root you pointed it at.
- Anything that lets a remote page or process reach the local HTTP server (CSRF, DNS rebinding, origin-check bypass) or exfiltrate data through it.
- Prompt-construction or provider-request bugs that send more than the selected, approved code block to a provider.
- Cache-key or config-parsing bugs that leak data across repositories or sessions.
- Vulnerable dependencies not already caught by `govulncheck` in CI.

Out of scope — this is how the tool is supposed to work, not a vulnerability:

- Reading arbitrary files **within** the repository directory you explicitly launched it on — that's the product.
- Sending a source block to OpenAI/Anthropic after you've reviewed and approved it — that's opt-in, per-block, using your own API key, and is documented in the README.
- The target repository containing secrets in source files you choose to approve for explanation — tlcr warns about this; review before approving.
- Terraform/IAM/security findings from the *code being read* — tlcr is a structural reader, not a scanner (see README's "Deliberate MVP limits").

## What's already in place

- The server binds to `127.0.0.1` only and is never exposed beyond localhost.
- Same-origin enforcement and a restrictive `Content-Security-Policy` on every response (`server.go`).
- No whole-repository upload and no background model calls; each explanation requires explicit per-block approval.
- API keys are read only from environment variables; `config.json` is gitignored and must never contain one.
- File reads resolve symlinks and verify the result stays inside the scanned root before serving it.
- `govulncheck` and `golangci-lint`'s `gosec` linter run in CI on every push/PR and weekly on a schedule.

## Local comprehension and review

Structural browsing, local Git evidence, Tours, and Change Tour require no credentials or network. Local Git commands have fixed read-only arguments, literal pathspecs, bounded output and timeouts; system/global Git configuration, optional locks, hooks, external diff/textconv execution, replacement objects and lazy object fetching are disabled where applicable. Historical source is filtered through the current exclusions before display. Missing objects cause an error; they are not fetched.

Analysis never executes repository source, build commands, parser plugins from the repository, or discovered linters. File opens are confined through `os.Root`, reject symlinks and special files, and cap reads. Snapshot invalidation checks prevent newly ignored source from being served through stale navigation. Per-file/aggregate/traversal limits prevent unbounded scans; they are not a sandbox for executing untrusted programs.

The AI preview is the exact bounded payload, including selected evidence. The send endpoint requires both explicit approval and a matching recomputed payload digest. Approved source can contain secrets not covered by exclusion rules; inspect the actual preview. Cached explanations remain local and can themselves be sensitive. No source is stored in browser local storage.

Syntax tokenization runs in a same-origin worker using vendored grammars and a timeout. Source rendering uses text nodes. The UI does not load a CDN, execute document HTML, or render model-provided scripts or images.
