# Docker Images

This directory contains Dockerfiles for foxctl build and runtime images.

The build context remains the repository root. Keep `.dockerignore` at the
repository root so Docker applies it when commands use `.` as the context.

## Images

- `Dockerfile` - production foxctl runtime image.
- `Dockerfile.ci` - GitLab CI image used by `.gitlab-ci.yml`.
- `Dockerfile.gui-auth-gateway` - public GUI auth gateway image.
- `Dockerfile.runner` - agent CI runner image.

## Examples

```bash
docker build -f deploy/docker/Dockerfile.ci .
docker build -f deploy/docker/Dockerfile .
```
