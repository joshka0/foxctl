package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuilderBuildsTerraformKubernetesAndShellComponents(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeFile := func(rel, body string) {
		path := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	writeFile("infra/main.tf", `
provider "aws" {}

resource "aws_s3_bucket" "app" {}

module "network" {
  source = "./modules/network"
}
`)
	writeFile("deploy/api.yaml", `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: default
---
apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: default
`)
	writeFile("scripts/deploy.sh", `#!/usr/bin/env bash
set -euo pipefail
kubectl apply -f deploy/api.yaml
terraform plan
echo "$NAMESPACE"
`)

	store, err := Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	builder := NewBuilder(store, workspace)
	result, err := builder.Build(ctx, BuildOptions{
		RepoRoot:          workspace,
		IncludeGo:         false,
		IncludeTypescript: false,
		IncludeElixir:     false,
		IncludeTerraform:  true,
		IncludeKubernetes: true,
		IncludeShell:      true,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.Files != 3 {
		t.Fatalf("files=%d want 3", result.Files)
	}
	if result.Packages < 3 {
		t.Fatalf("packages=%d want at least 3", result.Packages)
	}
	if result.Symbols < 6 {
		t.Fatalf("concept count=%d want at least 6", result.Symbols)
	}

	tfHits, err := store.SearchFTS(ctx, "aws_s3_bucket app", 10)
	if err != nil {
		t.Fatalf("search terraform concepts: %v", err)
	}
	if !containsNodeName(tfHits, "resource aws_s3_bucket.app") {
		t.Fatalf("expected terraform resource concept in %#v", tfHits)
	}

	k8sHits, err := store.SearchFTS(ctx, "Deployment api", 10)
	if err != nil {
		t.Fatalf("search kubernetes concepts: %v", err)
	}
	if !containsNodeName(k8sHits, "Deployment/default/api") {
		t.Fatalf("expected kubernetes deployment concept in %#v", k8sHits)
	}

	cmdHits, err := store.SearchFTS(ctx, "kubectl", 10)
	if err != nil {
		t.Fatalf("search shell commands: %v", err)
	}
	if !containsNodeName(cmdHits, "kubectl") {
		t.Fatalf("expected kubectl concept in %#v", cmdHits)
	}

	repoKey := store.RepoKey()
	shPkgID := infraPackageID(shellPkgPrefix, "scripts/deploy.sh")
	shFileID := FileID(repoKey, shPkgID, "scripts/deploy.sh")
	kubectlID := NamespacedID(repoKey, ConceptCommand+"kubectl")
	envID := NamespacedID(repoKey, ConceptEnvVar+"NAMESPACE")

	outgoing, err := store.GetOutgoingEdges(ctx, shFileID, []EdgeType{EdgeCalls, EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get outgoing edges: %v", err)
	}
	if !containsEdge(outgoing, shFileID, kubectlID, EdgeCalls) {
		t.Fatalf("expected CALLS edge %s -> %s", shFileID, kubectlID)
	}
	if !containsEdge(outgoing, shFileID, envID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", shFileID, envID)
	}
}

func containsNodeName(nodes []Node, want string) bool {
	for _, node := range nodes {
		if node.Name == want {
			return true
		}
	}
	return false
}
