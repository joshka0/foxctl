# Kubernetes Deployment

This directory contains Kubernetes manifests for deploying agentctl.

## Quick Start

```bash
# Create namespace and apply all resources
kubectl apply -k deploy/kubernetes/

# Or apply individually
kubectl apply -f deploy/kubernetes/namespace.yaml
kubectl apply -f deploy/kubernetes/secrets.yaml  # Edit first!
kubectl apply -f deploy/kubernetes/rbac.yaml
kubectl apply -f deploy/kubernetes/configmap.yaml
kubectl apply -f deploy/kubernetes/deployment.yaml
```

## Prerequisites

1. **Turso Database**: Create a Turso database and obtain credentials
2. **Voyage API Key**: Sign up at voyageai.com for embedding API access
3. **Container Image**: Build and push the agentctl image to your registry

## Configuration

### Required Secrets

Edit `secrets.yaml` before applying:

```yaml
stringData:
  turso-url: "libsql://your-database.turso.io"
  turso-token: "your-auth-token"
  voyage-key: "your-voyage-api-key"
```

### Optional: Git Credentials

For workspace deployments with git-sync:

```yaml
stringData:
  username: "git"
  token: "ghp_your_github_token"
```

## Components

| File | Description |
|------|-------------|
| `namespace.yaml` | Namespace definition |
| `secrets.yaml` | Secrets (edit before applying) |
| `rbac.yaml` | ServiceAccount, Role, RoleBinding |
| `configmap.yaml` | Configuration options |
| `deployment.yaml` | Core agentctl deployment |
| `workspace-deployment.yaml` | Workspace with git-sync sidecar |
| `embedding-worker.yaml` | Embedding job processor |
| `hpa.yaml` | Horizontal Pod Autoscaler |
| `pdb.yaml` | Pod Disruption Budgets |
| `cronjobs.yaml` | Scheduled maintenance jobs |
| `network-policy.yaml` | Network security policies |
| `kustomization.yaml` | Kustomize configuration |

## Workspace Access Patterns

### Read-Only (Recommended)

Use `workspace-deployment.yaml` with git-sync for analysis workloads:

```bash
# Customize repository URL and branch in workspace-deployment.yaml
kubectl apply -f deploy/kubernetes/workspace-deployment.yaml
```

### Read-Write

For code generation that needs to commit:

1. Use a PersistentVolumeClaim with ReadWriteMany access
2. Or generate patches stored in CAS/S3 and apply via separate PR creation job

## Scaling

The HPA scales based on CPU/memory. For custom metrics:

1. Deploy a metrics adapter (e.g., Prometheus Adapter)
2. Uncomment the custom metric section in `hpa.yaml`
3. Configure `agentctl_mailbox_depth` metric

## Monitoring

Pods expose metrics on port 8080. Prometheus annotations are included:

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"
  prometheus.io/path: "/metrics"
```

## Troubleshooting

### Check pod status

```bash
kubectl get pods -n agentctl
kubectl logs -n agentctl deployment/agentctl
```

### Verify database connectivity

```bash
kubectl exec -n agentctl deployment/agentctl -- agentctl health
```

### Check mailbox depth

```bash
kubectl exec -n agentctl deployment/agentctl -- agentctl mailbox stats
```

## Documentation

See [docs/kubernetes.md](../../docs/kubernetes.md) for detailed architecture documentation.
