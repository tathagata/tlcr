# Agent guide

Instructions for AI coding agents (and a useful checklist for humans) working in this repository.

## This repository is public

Everything pushed here is permanent and world-readable: file contents, commit messages, branch names, and the titles, descriptions and comments of issues and pull requests. A pull request's original commits stay visible on GitHub even after a force-push, so a mistake cannot be reliably undone. Check before you push, not after.

Never put any of the following in a file, commit message, PR or issue:

- **Agent session links or IDs.** No `claude.ai/code/session_…` URLs, `Claude-Session:` trailers, or any other link back to the conversation that produced a change. A `Co-Authored-By:` trailer is fine.
- **Other private repositories.** Do not name them, link to them, or describe their layout. Use a generic description ("a Terraform repository") or an invented fixture.
- **Personal contact details.** No personal email addresses or phone numbers. Security reports go through GitHub private vulnerability reporting (see `SECURITY.md`).
- **Local machine details.** No absolute paths containing a username or home directory, hostnames, or internal URLs. Use repository-relative paths.
- **Credentials or real identifiers.** No API keys or tokens (test doubles must be obviously fake, e.g. `test-not-a-real-key`), no real cloud account IDs, ARNs, IP addresses or domains. Use `example.com`, `example.invalid` and `127.0.0.1`.
- **Real data in fixtures.** Write fixtures by hand; do not copy them from another project, private or otherwise, unless its license allows it and the source is credited in `docs/THIRD_PARTY.md`.

Before committing, read the full diff and the commit message for the items above. If you are unsure whether something is private, leave it out and say so in your summary.

## Working here

- Changes reach `main` through a pull request; do not push to `main` directly.
- There may be no Go toolchain on the host. `make test`, `make vet`, `make fmt` and `make tidy` run in Docker; `make test-ui` runs the JavaScript tests.
- CI must pass: `gofmt`, `go vet`, `golangci-lint`, `go mod tidy -diff`, `govulncheck`, `go test -race` on Linux, macOS and Windows, and the UI tests.
- Runtime behavior stays local and read-only toward the target repository. Do not add network calls, telemetry, CDN assets, or execution of repository content. A new dependency needs a reason, and vendored assets need an entry in `docs/THIRD_PARTY.md`.
