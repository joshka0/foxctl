---
title: Kubernetes and deployment
description: Deployment references for Kubernetes, runtime services, and production operations.
---

Status: Current shell, deployment details remain canonical in guides and architecture docs.

Deployment docs should stay operational and source-controlled. Avoid duplicating
large manifests in Starlight; link to the canonical guide and deployment tree.

## Read first

- [docs/guides/kubernetes.md](https://github.com/joshka0/foxctl/blob/main/docs/guides/kubernetes.md)
- [docs/architecture/kubernetes-runtime.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/kubernetes-runtime.md)
- [deploy/kubernetes](https://github.com/joshka0/foxctl/tree/main/deploy/kubernetes)

## Production checklist

- Confirm which runtime service is being deployed.
- Confirm storage driver and secret source.
- Confirm network and path policy constraints.
- Confirm observability output and event retention.
- Confirm rollback path before applying manifests.

## Related surfaces

```bash
foxctl mcp serve --daemon --skills
```

```bash
foxctl web serve --help
```

## Canonical sources

- [docs/guides/kubernetes.md](https://github.com/joshka0/foxctl/blob/main/docs/guides/kubernetes.md)
- [docs/architecture/kubernetes-runtime.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/kubernetes-runtime.md)
- [docs/general/api-server.md](https://github.com/joshka0/foxctl/blob/main/docs/general/api-server.md)
- [docs/general/agent-policy-and-prompts.md](https://github.com/joshka0/foxctl/blob/main/docs/general/agent-policy-and-prompts.md)

