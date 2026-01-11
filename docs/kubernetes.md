# Kubernetes Deployment Guide

This document describes how to deploy agentctl in Kubernetes, covering
architecture patterns, storage considerations, and operational best practices.

## Overview

agentctl's architecture is designed for distributed deployment with:

- **Turso embedded replicas** for low-latency reads with cloud sync
- **Stateless supervisors** that share a common mailbox
- **Workspace-scoped sessions** enabling multi-tenant isolation
- **Content-addressable storage** for artifact deduplication

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Kubernetes Cluster                                │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐         │
│  │  agentctl-pod-1 │    │  agentctl-pod-2 │    │  agentctl-pod-n │         │
│  │  ┌───────────┐  │    │  ┌───────────┐  │    │  ┌───────────┐  │         │
│  │  │ Supervisor│  │    │  │ Supervisor│  │    │  │ Supervisor│  │         │
│  │  │ (Actors)  │  │    │  │ (Actors)  │  │    │  │ (Actors)  │  │         │
│  │  └─────┬─────┘  │    │  └─────┬─────┘  │    │  └─────┬─────┘  │         │
│  │        │        │    │        │        │    │        │        │         │
│  │  ┌─────▼─────┐  │    │  ┌─────▼─────┐  │    │  ┌─────▼─────┐  │         │
│  │  │  Turso    │  │    │  │  Turso    │  │    │  │  Turso    │  │         │
│  │  │ Embedded  │  │    │  │ Embedded  │  │    │  │ Embedded  │  │         │
│  │  │ Replica   │  │    │  │ Replica   │  │    │  │ Replica   │  │         │
│  │  └─────┬─────┘  │    │  └─────┬─────┘  │    │  └─────┬─────┘  │         │
│  └────────┼────────┘    └────────┼────────┘    └────────┼────────┘         │
│           │                      │                      │                   │
│           └──────────────────────┼──────────────────────┘                   │
│                                  │                                          │
│  ┌───────────────────────────────▼───────────────────────────────────────┐  │
│  │                     Turso Cloud (Primary)                             │  │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌───────────┐ ┌───────────────┐  │  │
│  │  │tasks.db │ │sessions │ │memory.db│ │mailbox.db │ │blackboard.db  │  │  │
│  │  │         │ │.db      │ │+vectors │ │           │ │               │  │  │
│  │  └─────────┘ └─────────┘ └─────────┘ └───────────┘ └───────────────┘  │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                         External Services                             │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌──────────────┐  │  │
│  │  │  Voyage AI  │  │   MinIO/S3  │  │    NATS     │  │   Redis      │  │  │
│  │  │ (embeddings)│  │   (CAS)     │  │ (optional)  │  │  (optional)  │  │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘  └──────────────┘  │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Workspace Access Patterns

agentctl requires access to source code for analysis and modification. There are
several patterns for providing workspace access in Kubernetes.

### Pattern 1: Git-Sync Sidecar (Recommended for Read-Heavy)

Best for: Analysis, code review, complexity metrics, semantic search.

```
┌──────────────────────────────────────────────────────────────────┐
│ Pod                                                              │
│  ┌─────────────┐    ┌─────────────┐    ┌──────────────────┐      │
│  │  agentctl   │◄──►│  /workspace │◄───│  git-sync        │      │
│  │  container  │    │  (emptyDir) │    │  sidecar         │      │
│  └─────────────┘    └─────────────┘    └────────┬─────────┘      │
└─────────────────────────────────────────────────┼────────────────┘
                                                  │
                                            ┌─────▼─────┐
                                            │  GitHub   │
                                            │  GitLab   │
                                            └───────────┘
```

**Advantages:**
- Always fresh code (configurable sync interval)
- No PVC required
- Scales horizontally
- Clean separation of concerns

**Limitations:**
- Read-only (cannot push changes)
- Sync delay (30s default)
- Large repos have slow initial clone

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentctl-workspace
spec:
  replicas: 3
  selector:
    matchLabels:
      app: agentctl
  template:
    metadata:
      labels:
        app: agentctl
    spec:
      volumes:
      - name: workspace
        emptyDir: {}

      containers:
      - name: agentctl
        image: agentctl:latest
        volumeMounts:
        - name: workspace
          mountPath: /workspace
          readOnly: true
        env:
        - name: AGENTCTL_WORKSPACE
          value: "/workspace/current"
        - name: AGENTCTL_DB_DRIVER
          value: "turso"
        - name: TURSO_URL
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: turso-url
        - name: TURSO_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: turso-token

      - name: git-sync
        image: registry.k8s.io/git-sync/git-sync:v4.2.1
        args:
        - --repo=https://github.com/org/repo
        - --root=/workspace
        - --period=30s
        - --link=current
        - --ref=main
        - --depth=1
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        env:
        - name: GITSYNC_USERNAME
          valueFrom:
            secretKeyRef:
              name: git-creds
              key: username
        - name: GITSYNC_PASSWORD
          valueFrom:
            secretKeyRef:
              name: git-creds
              key: token
```

### Pattern 2: Persistent Volume (For Write Access)

Best for: Code generation, refactoring, commit creation.

```
┌──────────────────────────────────────────────────────────────────┐
│ Pod                                                              │
│  ┌─────────────┐    ┌─────────────────────────────────────────┐  │
│  │  agentctl   │◄──►│  PVC: workspace-myrepo-pvc              │  │
│  │  container  │    │  (ReadWriteMany NFS/EFS)                │  │
│  └─────────────┘    └─────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

**Advantages:**
- Full read/write access
- Persists across pod restarts
- Multiple pods can share workspace

**Limitations:**
- Requires RWX-capable storage (NFS, EFS, etc.)
- Higher cost
- Potential git conflicts between pods

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: workspace-myrepo
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
  storageClassName: efs-sc
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentctl-writer
spec:
  replicas: 1  # Single writer to avoid conflicts
  template:
    spec:
      volumes:
      - name: workspace
        persistentVolumeClaim:
          claimName: workspace-myrepo
      containers:
      - name: agentctl
        image: agentctl:latest
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        env:
        - name: AGENTCTL_WORKSPACE
          value: "/workspace"
```

### Pattern 3: Ephemeral Job (Task-Based)

Best for: CI/CD integration, one-off analysis, pull request checks.

```
┌──────────────────────────────────────────────────────────────────┐
│ Job                                                              │
│  ┌─────────────┐    ┌─────────────┐                              │
│  │  init:      │───►│  agentctl   │                              │
│  │  git clone  │    │  (runs task)│                              │
│  └─────────────┘    └─────────────┘                              │
└──────────────────────────────────────────────────────────────────┘
                          ▼
                    Pod terminates
```

**Advantages:**
- Clean state every run
- No persistent storage needed
- Natural fit for CI/CD

**Limitations:**
- Cold start overhead
- No state between runs

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: agentctl-analyze-pr-123
spec:
  template:
    spec:
      initContainers:
      - name: clone
        image: alpine/git
        command:
        - sh
        - -c
        - |
          git clone --depth=1 --branch=feature-xyz \
            https://github.com/org/repo /workspace
        volumeMounts:
        - name: workspace
          mountPath: /workspace

      containers:
      - name: agentctl
        image: agentctl:latest
        command:
        - agentctl
        - run
        - code/complexity
        - --input
        - '{"path": "/workspace"}'
        volumeMounts:
        - name: workspace
          mountPath: /workspace
        env:
        - name: AGENTCTL_DB_DRIVER
          value: "turso"
        - name: TURSO_URL
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: turso-url

      volumes:
      - name: workspace
        emptyDir: {}

      restartPolicy: Never
  backoffLimit: 2
```

### Pattern 4: Hybrid (Recommended for Production)

Best for: Production deployments requiring both read and write capabilities.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                             │
│   ┌───────────────────────────────────────────────────────────────────┐    │
│   │                     Workspace Controller                          │    │
│   │   (Custom operator that manages workspace lifecycle)              │    │
│   └───────────────────────────────┬───────────────────────────────────┘    │
│                                   │                                         │
│           ┌───────────────────────┼───────────────────────────┐            │
│           │                       │                           │            │
│           ▼                       ▼                           ▼            │
│   ┌───────────────┐      ┌───────────────┐      ┌───────────────┐          │
│   │ Workspace Pod │      │ Workspace Pod │      │ Workspace Pod │          │
│   │ repo: foo     │      │ repo: bar     │      │ repo: baz     │          │
│   │ branch: main  │      │ branch: feat  │      │ branch: main  │          │
│   ├───────────────┤      ├───────────────┤      ├───────────────┤          │
│   │ git-sync      │      │ git-sync      │      │ git-sync      │          │
│   │ agentctl      │      │ agentctl      │      │ agentctl      │          │
│   │ supervisor    │      │ supervisor    │      │ supervisor    │          │
│   └───────┬───────┘      └───────┬───────┘      └───────┬───────┘          │
│           │                      │                      │                  │
│           └──────────────────────┼──────────────────────┘                  │
│                                  │                                          │
│                          ┌───────▼───────┐                                  │
│                          │  Turso Cloud  │                                  │
│                          │  (shared DB)  │                                  │
│                          └───────────────┘                                  │
│                                                                             │
│   ┌───────────────────────────────────────────────────────────────────┐    │
│   │                       Write Operations                            │    │
│   │  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐       │    │
│   │  │   agentctl   │────►│  CAS Store   │────►│  PR Creator  │       │    │
│   │  │   (patches)  │     │  (S3)        │     │  (Job)       │       │    │
│   │  └──────────────┘     └──────────────┘     └──────┬───────┘       │    │
│   │                                                   │               │    │
│   │                                           ┌───────▼───────┐       │    │
│   │                                           │    GitHub     │       │    │
│   │                                           │    API        │       │    │
│   │                                           └───────────────┘       │    │
│   └───────────────────────────────────────────────────────────────────┘    │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key Design:**
- One pod per (repo, branch) tuple
- Read operations via git-sync sidecar
- Write operations generate patches stored in CAS
- Separate job applies patches via GitHub API
- Workspace pods remain stateless and read-only

## Component Architecture

### Database Layer: Turso Embedded Replicas

agentctl uses Turso's embedded replica pattern for optimal k8s deployment:

```
Pod Local                    Cloud
┌──────────┐     sync       ┌──────────┐
│ Embedded │ ◄────────────► │  Turso   │
│ Replica  │                │  Primary │
└──────────┘                └──────────┘
   (reads)                    (writes)
```

**How it works:**
- **Reads**: Served from local embedded replica (~0ms latency)
- **Writes**: Sent to primary, synced back to replicas
- **Conflicts**: Turso handles via causal ordering

**Databases:**

| Database | Purpose | Vector Support |
|----------|---------|----------------|
| `tasks.db` | Task management | No |
| `sessions.db` | Session lineage | Optional |
| `memory.db` | Named memories | Yes (1024 dims) |
| `mailbox.db` | Message queue | No |
| `blackboard.db` | Coordination | No |
| `trajectory.db` | Audit log | No |

### Actor System: Distributed Supervisor

The actor system is designed for multi-pod deployment:

```
Pod 1: Supervisor ──┐
                    │      ┌──────────────┐
Pod 2: Supervisor ──┼─────►│ mailbox.db   │ (Turso)
                    │      │ (shared)     │
Pod 3: Supervisor ──┘      └──────────────┘
```

**Properties:**
- Each pod runs one Supervisor instance
- All supervisors poll the same mailbox
- Atomic claim via `visible_at` prevents double-processing
- Lease-based semantics enable crash recovery
- Actors can migrate between pods on restart

**Message contract:**

```go
type Message struct {
    ID        string          // Unique message ID
    FromNS    string          // Source agent namespace
    ToNS      string          // Target agent namespace
    Type      MessageType     // ask|reply|cmd|event
    Payload   json.RawMessage // Envelope format
    SessionID string          // For lineage tracking
    Workspace string          // Scope isolation
    AgentID   string          // Sending agent
    VisibleAt int64           // Lease timeout
    Attempt   int             // Retry count
}
```

### Content-Addressable Storage (CAS)

For k8s deployment, CAS should use S3-compatible storage:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  agentctl pod   │────►│  MinIO / S3     │────►│  CDN (optional) │
│  (store/get)    │     │  bucket: cas    │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

**Benefits:**
- Unlimited storage
- Cross-pod sharing without PVCs
- Content deduplication preserved
- CDN-friendly for artifact distribution

**Implementation interface:**

```go
type CASStore interface {
    Get(ctx context.Context, digest string) ([]byte, error)
    Put(ctx context.Context, data []byte) (digest string, error)
    Has(ctx context.Context, digest string) (bool, error)
    Delete(ctx context.Context, digest string) error
}
```

### Embeddings: Batch Processing

For cost efficiency, embeddings should be processed via a job queue:

```
┌────────────┐     ┌─────────────────┐     ┌────────────┐
│ Code Change│────►│ embedding/queue │────►│ Voyage API │
│ (skill)    │     │ (blackboard)    │     │            │
└────────────┘     └────────┬────────┘     └──────┬─────┘
                            │                     │
                   ┌────────▼────────┐            │
                   │ Embedding Worker│◄───────────┘
                   │ (dedicated pod) │
                   └─────────────────┘
```

**Scope-based model selection:**

| Scope | Content Type | Model | Price/1M | Dimensions |
|-------|--------------|-------|----------|------------|
| `symbols` | Code | `voyage-code-3` | $0.18 | 1024 |
| `memory` | Gotchas/notes | `voyage-3-large` | $0.06 | 1024 |
| `tasks` | Task descriptions | `voyage-3.5` | $0.06 | 1024 |
| `sessions` | Session context | `voyage-3.5` | $0.06 | 1024 |

### Session Management

Sessions are workspace-scoped, enabling multi-tenant isolation:

```
Pod 1                      Pod 2
┌─────────────────┐       ┌─────────────────┐
│ Session A       │       │ Session B       │
│ workspace: /app │       │ workspace: /app │
│ agent: claude   │       │ agent: gemini   │
└────────┬────────┘       └────────┬────────┘
         │                         │
         └─────────┬───────────────┘
                   │
         ┌─────────▼─────────┐
         │ sessions.db       │
         │ GetActive() →     │
         │ only one per      │
         │ (workspace,agent) │
         └───────────────────┘
```

**Key constraint:** `GetActive(workspace, agentID)` returns only one session,
preventing conflicts in multi-pod scenarios.

**Lineage tracking:**
- `parent_session_id` links forked sessions
- `GetAncestorChain()` retrieves full lineage
- Edge types: `continues`, `forked_from`, `relates_to`

## Deployment Manifests

### Core Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentctl
  labels:
    app: agentctl
spec:
  replicas: 3
  selector:
    matchLabels:
      app: agentctl
  template:
    metadata:
      labels:
        app: agentctl
    spec:
      serviceAccountName: agentctl

      containers:
      - name: agentctl
        image: agentctl:latest
        ports:
        - name: health
          containerPort: 8080

        env:
        # Database configuration
        - name: AGENTCTL_DB_DRIVER
          value: "turso"
        - name: TURSO_URL
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: turso-url
        - name: TURSO_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: turso-token

        # Embedding configuration
        - name: VOYAGE_API_KEY
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: voyage-key

        # CAS configuration
        - name: AGENTCTL_CAS_BACKEND
          value: "s3"
        - name: AGENTCTL_CAS_BUCKET
          value: "agentctl-cas"
        - name: AWS_REGION
          value: "us-west-2"

        # Observability
        - name: AGENTCTL_OBS_DIR
          value: "/var/log/agentctl"
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "http://otel-collector:4317"

        # Pod identity
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace

        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "1Gi"
            cpu: "500m"

        livenessProbe:
          exec:
            command: ["agentctl", "health"]
          initialDelaySeconds: 10
          periodSeconds: 30
          timeoutSeconds: 5

        readinessProbe:
          exec:
            command: ["agentctl", "health", "--ready"]
          initialDelaySeconds: 5
          periodSeconds: 10
          timeoutSeconds: 3

        volumeMounts:
        - name: cache
          mountPath: /var/cache/agentctl
        - name: logs
          mountPath: /var/log/agentctl

      volumes:
      - name: cache
        emptyDir:
          sizeLimit: 1Gi
      - name: logs
        emptyDir:
          sizeLimit: 500Mi
```

### Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: agentctl-secrets
type: Opaque
stringData:
  turso-url: "libsql://your-db.turso.io"
  turso-token: "your-auth-token"
  voyage-key: "your-voyage-api-key"
---
apiVersion: v1
kind: Secret
metadata:
  name: git-creds
type: Opaque
stringData:
  username: "git"
  token: "ghp_your_github_token"
```

### Service Account and RBAC

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: agentctl
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: agentctl
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["secrets"]
  resourceNames: ["agentctl-secrets", "git-creds"]
  verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: agentctl
subjects:
- kind: ServiceAccount
  name: agentctl
roleRef:
  kind: Role
  name: agentctl
  apiGroup: rbac.authorization.k8s.io
```

### Horizontal Pod Autoscaler

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: agentctl
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: agentctl
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 10
        periodSeconds: 60
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
      - type: Percent
        value: 100
        periodSeconds: 15
```

### PageRank CronJob

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: agentctl-pagerank
spec:
  schedule: "0 * * * *"  # Hourly
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: pagerank
            image: agentctl:latest
            command:
            - agentctl
            - overseer
            - rerank
            env:
            - name: AGENTCTL_DB_DRIVER
              value: "turso"
            - name: TURSO_URL
              valueFrom:
                secretKeyRef:
                  name: agentctl-secrets
                  key: turso-url
            - name: TURSO_AUTH_TOKEN
              valueFrom:
                secretKeyRef:
                  name: agentctl-secrets
                  key: turso-token
          restartPolicy: OnFailure
```

### Embedding Worker

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agentctl-embedding-worker
spec:
  replicas: 2
  selector:
    matchLabels:
      app: agentctl-embedding-worker
  template:
    metadata:
      labels:
        app: agentctl-embedding-worker
    spec:
      containers:
      - name: worker
        image: agentctl:latest
        command:
        - agentctl
        - worker
        - embeddings
        - --concurrency=5
        - --batch-size=100
        env:
        - name: AGENTCTL_DB_DRIVER
          value: "turso"
        - name: TURSO_URL
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: turso-url
        - name: TURSO_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: turso-token
        - name: VOYAGE_API_KEY
          valueFrom:
            secretKeyRef:
              name: agentctl-secrets
              key: voyage-key
        resources:
          requests:
            memory: "512Mi"
            cpu: "200m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
```

## Workspace Custom Resource (Future)

For production deployments, a Workspace CRD provides declarative management:

```yaml
apiVersion: agentctl.io/v1alpha1
kind: Workspace
metadata:
  name: myrepo-main
  namespace: agentctl
spec:
  # Repository configuration
  repository:
    url: https://github.com/org/myrepo
    branch: main
    credentials:
      secretName: git-creds

  # Sync configuration
  sync:
    period: 30s
    depth: 1

  # Resource limits
  resources:
    requests:
      memory: 512Mi
      cpu: 250m
    limits:
      memory: 2Gi
      cpu: 1000m

  # Scaling configuration
  scaling:
    minReplicas: 1
    maxReplicas: 5
    metrics:
    - type: mailbox-depth
      target: 100

  # Write mode configuration
  writeMode:
    enabled: false  # Read-only by default
    patchStore: s3://agentctl-patches

status:
  phase: Running
  podName: workspace-myrepo-main-abc123
  lastSync: "2025-01-15T10:30:00Z"
  conditions:
  - type: Ready
    status: "True"
    lastTransitionTime: "2025-01-15T10:00:00Z"
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AGENTCTL_DB_DRIVER` | Yes | `sqlite` | Database driver: `sqlite`, `turso` |
| `TURSO_URL` | If Turso | - | Turso database URL |
| `TURSO_AUTH_TOKEN` | If Turso | - | Turso authentication token |
| `VOYAGE_API_KEY` | For embeddings | - | Voyage AI API key |
| `AGENTCTL_CAS_BACKEND` | No | `file` | CAS backend: `file`, `s3` |
| `AGENTCTL_CAS_BUCKET` | If S3 | - | S3 bucket for CAS |
| `AGENTCTL_WORKSPACE` | No | - | Override workspace path |
| `AGENTCTL_OBS_DIR` | No | - | Observability events directory |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | - | OpenTelemetry collector endpoint |
| `POD_NAME` | Recommended | - | Kubernetes pod name |
| `POD_NAMESPACE` | Recommended | - | Kubernetes namespace |

## Migration Priorities

| Priority | Component | Effort | Notes |
|----------|-----------|--------|-------|
| **P0** | Turso migration | Low | Already implemented, enable it |
| **P1** | S3 CAS adapter | Medium | New store implementation needed |
| **P1** | Health endpoints | Low | Add `/health` and `/ready` |
| **P2** | Embedding worker | Medium | Separate from main pods |
| **P2** | Workspace controller | High | Custom operator for lifecycle |
| **P3** | NATS for mailbox | High | Only if latency is critical |
| **P3** | OpenTelemetry | Medium | Distributed tracing |

## Operational Considerations

### Monitoring

Key metrics to track:

- **Mailbox depth**: Messages pending processing
- **Actor count**: Active actors per pod
- **Embedding queue**: Pending embedding jobs
- **Session count**: Active sessions per workspace
- **CAS hit rate**: Cache effectiveness

### Graceful Shutdown

agentctl's supervisor implements graceful shutdown:

1. Stop accepting new messages
2. Wait for active actors to complete (configurable timeout)
3. Persist any in-flight state
4. Exit cleanly

Configure pod termination:

```yaml
spec:
  terminationGracePeriodSeconds: 60
  containers:
  - name: agentctl
    lifecycle:
      preStop:
        exec:
          command: ["agentctl", "drain"]
```

### Backup and Recovery

- **Turso**: Built-in replication and point-in-time recovery
- **CAS (S3)**: Enable versioning and cross-region replication
- **Sessions**: Lineage enables session reconstruction

### Security

- Use network policies to restrict pod communication
- Mount secrets as files, not environment variables (for rotation)
- Enable Pod Security Standards
- Use read-only root filesystem where possible

## Related Documentation

- [Architecture](architecture.md): Package organization and layers
- [Turso Migration](turso-migration.md): Database migration guide
- [Observability](observability/wide-events.md): Event logging
- [Security](SECURITY.md): Security considerations
