# Public gui-agent overlay

This overlay exposes `gui-agent` publicly through `gui-auth-gateway` and keeps
the Go `foxctl` service private inside the cluster.

It composes `../postgres` and adds:

- a public Better Auth gateway deployment and service
- gateway config and secrets
- a gateway-specific network policy
- an ingress patch routing `/` to `gui-auth-gateway`

## Usage

```bash
kubectl apply -k deploy/kubernetes/overlays/public-gui
```

## Notes

- Replace `BETTER_AUTH_URL` in `gateway-configmap.yaml`.
- Replace `better-auth-secret` and SMTP secrets in `gateway-secrets.yaml`.
- This overlay intentionally scales the core `foxctl` deployment to `1`
  replica to keep the current in-process SSE and console session behavior
  predictable for the public GUI path.
