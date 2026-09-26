# Optional GitHub evidence: design boundary

Status: design only. No remote provider is implemented or needed by local Change Tour.

A future GitHub provider should be requested explicitly for one chosen PR. Show the destination host, repository/PR and requested data before networking. Never infer permission to fetch merely from a Git remote. Avoid any write-scoped operation: reading PR metadata and existing comments must not post reviews, react, resolve threads, or update issues.

Use an explicitly selected authenticated account/session with the minimum read permissions. Tokens belong to the credential mechanism, never repository config, logs, URLs or source prompts. GitHub Enterprise hosts require explicit configuration and normal TLS verification; reject redirects across hosts or to private/local addresses when configured for public GitHub. Offline, unavailable auth, rate limits and deleted PRs yield a labeled unavailable provider while retaining complete local review functionality.

Correlate comments against repository identity, PR number, base/head commit IDs, path, diff side and original/current line metadata. Show outdated/unmatched comments separately. Do not attach a comment to an arbitrary nearby symbol after a rename or force push. Display the comment's author, timestamp, URL and associated commit; these are external observations, not compiler-derived facts or proof of correctness. Recency should be explicit, with a user-triggered refresh and no silent polling.

Remote Markdown and quoted code remain untrusted text. No embedded remote images or HTML execution. Comments may contain prompt injection; optional AI context must label them as external content, cap them separately, and include their exact text in the normal payload preview. Do not store tokens, source, comments or author metadata in persistent caches by default. A future opt-in cache needs documented retention and a clear deletion action.

Required fixtures before implementation: offline/no-auth/non-GitHub cases, missing/old comments, rebased and renamed paths, duplicate line matches, fork PRs, force-pushed heads, rate-limit/cancellation responses, redirects, hostile Markdown and prompt text. Tests must prove no mutation endpoint is reachable and no network occurs without the explicit remote-evidence action.
