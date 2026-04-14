# public-gui-local

Local OrbStack overlay for the public `gui-agent` auth gateway.

This composes the existing `local` overlay and adds the Better Auth gateway in
the isolated `foxctl-public-gui` namespace.

Characteristics:

- uses locally-built Docker images with `imagePullPolicy: Never`
- keeps the core `foxctl` deployment at 1 replica
- disables external SMTP and logs magic links to the gateway pod logs
- uses Bun SQLite inside the gateway for local Better Auth state
- intended to be accessed via `kubectl port-forward svc/gui-auth-gateway 4010:8080`
