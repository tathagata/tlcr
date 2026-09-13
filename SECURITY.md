# Security Policy

CodeRead is a personal, single-maintainer project. There's no dedicated security team and no formal SLA, but reports are taken seriously and triaged promptly.

## Supported versions

There are no numbered releases yet — only the tip of `main` is supported. Security fixes land there; build a fresh binary (or download the latest **Test and build** Actions artifact) to pick them up.

## Reporting a vulnerability

Please **do not open a public GitHub issue** for a suspected vulnerability.

1. Preferred: use GitHub's private vulnerability reporting for this repo, if enabled — go to the **Security** tab → **Report a vulnerability**.
2. Otherwise: email **tathagatadg@gmail.com** with a description and, if possible, steps to reproduce.

You should get an acknowledgment within a few days. Fixes ship as soon as practical given this is maintained in spare time; there's no guaranteed patch window.

## Scope

In scope — bugs in CodeRead itself that would let it do something it isn't supposed to:

- Path traversal or symlink escapes that let it read files outside the repository root you pointed it at.
- Anything that lets a remote page or process reach the local HTTP server (CSRF, DNS rebinding, origin-check bypass) or exfiltrate data through it.
- Prompt-construction or provider-request bugs that send more than the selected, approved code block to a provider.
- Cache-key or config-parsing bugs that leak data across repositories or sessions.
- Vulnerable dependencies not already caught by `govulncheck` in CI.

Out of scope — this is how the tool is supposed to work, not a vulnerability:

- Reading arbitrary files **within** the repository directory you explicitly launched it on — that's the product.
- Sending a source block to OpenAI/Anthropic after you've reviewed and approved it — that's opt-in, per-block, using your own API key, and is documented in the README.
- The target repository containing secrets in source files you choose to approve for explanation — CodeRead warns about this; review before approving.
- Terraform/IAM/security findings from the *code being read* — CodeRead is a structural reader, not a scanner (see README's "Deliberate MVP limits").

## What's already in place

- The server binds to `127.0.0.1` only and is never exposed beyond localhost.
- Same-origin enforcement and a restrictive `Content-Security-Policy` on every response (`server.go`).
- No whole-repository upload and no background model calls; each explanation requires explicit per-block approval.
- API keys are read only from environment variables; `config.json` is gitignored and must never contain one.
- File reads resolve symlinks and verify the result stays inside the scanned root before serving it.
- `govulncheck` and `golangci-lint`'s `gosec` linter run in CI on every push/PR and weekly on a schedule.
