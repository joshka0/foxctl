---
title: Gotchas and operating rules
description: Common foxctl pitfalls and the rules that prevent avoidable breakage.
---

Status: Current shell.

This page is the short production entrypoint. The canonical gotchas files remain
the full source.

## Frequent rules

| Rule | Why it matters |
|---|---|
| Prefer `./bin/foxctl` in this checkout when PATH is ambiguous | Avoids bundled-wrapper command gaps |
| Preserve JSON envelope shape | Hooks, GUIs, and golden tests depend on it |
| Keep WASI skill network policy at `network: "none"` | Maintains Core v1 isolation |
| Use CAS for large outputs | Prevents bloated envelopes and memory pressure |
| Use structured shell for noisy read-only retrieval | Keeps agent context compact |
| Do not route behavior with keyword heuristics | Avoids brittle hidden policy |
| Run `make check-doc-links` after markdown changes | CI enforces docs link hygiene |

## Current docs-site gotchas

- `site` is not set in `astro.config.mjs`, so Starlight sitemap generation is
  skipped until the production hostname is chosen.
- `bun audit` may still fail for pre-existing non-docs workspaces. The docs-site
  gate is no `workspace:@foxctl/docs-site` findings.
- Do not add Starlight plugins or themes without Socket/SFW vetting.

## Canonical sources

- [docs/general/gotchas.md](https://github.com/joshka0/foxctl/blob/main/docs/general/gotchas.md)
- [docs/start/gotchas.md](https://github.com/joshka0/foxctl/blob/main/docs/start/gotchas.md)
- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
- [docs/DOC_LIFECYCLE.md](https://github.com/joshka0/foxctl/blob/main/docs/DOC_LIFECYCLE.md)

