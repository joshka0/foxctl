# sandbox/echo

Test skill that executes commands inside a k8s agent-sandbox pod. Used to
verify the sandbox integration end-to-end.

## Usage

```bash
export FOXCTL_K8S_SANDBOX_WARMPOOL=foxctl-test-pool
export FOXCTL_K8S_SANDBOX_NAMESPACE=agent-sandbox-demo
export FOXCTL_K8S_SANDBOX_MODE=direct
export FOXCTL_K8S_SANDBOX_API_URL=http://<pod-ip>:8888
foxctl run sandbox/echo --input '{"message":"hello"}'
```
