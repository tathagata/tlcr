# CodeRead

An on-demand, read-only code reader: source on the left, generated explanation on the right. The CLI and browser UI ship in one Go binary. Terraform/HCL and Go are parsed structurally; frontend assets are viewable as files. No code goes to an AI provider until you select a block and approve the request.

## Quick start

Download the binary for your platform from a successful **Test and build** GitHub Actions run (Artifacts), or build locally with Go 1.23+:

```sh
go mod tidy
go build -o coderead .
./coderead /path/to/repo
```

This opens a browser bound to `127.0.0.1` on an available port. You can also use `./coderead --no-browser .` and open the printed URL yourself. Close the process with Ctrl-C.

The structural view needs no model and works without a config file. To enable explanations, copy `config.example.json` to `config.json` **beside the binary**, set `provider` to `openai` or `anthropic`, and set `model` to a model available to your API account. Export the matching `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`. You can instead pass `--config /path/to/config.json`. API access is billed by your provider; a ChatGPT or Claude subscription is not necessarily an API account.

`config.json` is intentionally gitignored; do not put API keys in it. The example leaves `model` blank so you choose an available model deliberately. The UI asks for confirmation each time before sending the highlighted block. No entire-repository upload or background generation occurs. The token limits are **estimates and safeguards**, not a guaranteed billing cap; provider-reported usage is shown after each call.

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
