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

variable "env" {}

locals {
  bucket_name = var.env
}

resource "aws_s3_bucket" "app" {
  bucket = local.bucket_name
}

output "bucket_arn" {
  value = aws_s3_bucket.app.arn
}

output "network_vpc_id" {
  value = module.network.vpc_id
}

module "network" {
  source = "./modules/network"
  cidr   = local.bucket_name
}
`)
	writeFile("infra/modules/network/main.tf", `
variable "cidr" {}
resource "aws_vpc" "main" {}
output "vpc_id" {
  value = aws_vpc.main.id
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
	if result.Files != 4 {
		t.Fatalf("files=%d want 4", result.Files)
	}
	if result.Packages < 4 {
		t.Fatalf("packages=%d want at least 4", result.Packages)
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
	if !containsNodeFile(tfHits, "infra/main.tf") {
		t.Fatalf("expected terraform concept to retain file path in %#v", tfHits)
	}

	k8sHits, err := store.SearchFTS(ctx, "Deployment api", 10)
	if err != nil {
		t.Fatalf("search kubernetes concepts: %v", err)
	}
	if !containsNodeName(k8sHits, "Deployment/default/api") {
		t.Fatalf("expected kubernetes deployment concept in %#v", k8sHits)
	}
	if !containsNodeFile(k8sHits, "deploy/api.yaml") {
		t.Fatalf("expected kubernetes concept to retain file path in %#v", k8sHits)
	}

	cmdHits, err := store.SearchFTS(ctx, "kubectl", 10)
	if err != nil {
		t.Fatalf("search shell commands: %v", err)
	}
	if !containsNodeName(cmdHits, "kubectl") {
		t.Fatalf("expected kubectl concept in %#v", cmdHits)
	}

	repoKey := store.RepoKey()
	tfPkgID := infraPackageID(terraformPkgPrefix, "infra/main.tf")
	moduleNetworkID := terraformConceptNodeID(repoKey, tfPkgID, "module:network")
	networkPkgNodeID := PackageID(repoKey, infraPackageIDFromDir(terraformPkgPrefix, "infra/modules/network"))
	networkVarCIDRID := terraformConceptNodeID(repoKey, infraPackageIDFromDir(terraformPkgPrefix, "infra/modules/network"), "variable:cidr")
	networkOutputVpcID := terraformConceptNodeID(repoKey, infraPackageIDFromDir(terraformPkgPrefix, "infra/modules/network"), "output:vpc_id")
	tfPkgNodeID := PackageID(repoKey, tfPkgID)
	localBucketID := terraformConceptNodeID(repoKey, tfPkgID, "local:bucket_name")
	varEnvID := terraformConceptNodeID(repoKey, tfPkgID, "variable:env")
	resourceBucketID := terraformConceptNodeID(repoKey, tfPkgID, "resource:aws_s3_bucket.app")
	outputBucketArnID := terraformConceptNodeID(repoKey, tfPkgID, "output:bucket_arn")
	outputNetworkVpcID := terraformConceptNodeID(repoKey, tfPkgID, "output:network_vpc_id")
	shPkgID := infraPackageID(shellPkgPrefix, "scripts/deploy.sh")
	shFileID := FileID(repoKey, shPkgID, "scripts/deploy.sh")
	kubectlID := NamespacedID(repoKey, ConceptCommand+"kubectl")
	envID := NamespacedID(repoKey, ConceptEnvVar+"NAMESPACE")
	tfEdges, err := store.GetOutgoingEdges(ctx, moduleNetworkID, []EdgeType{EdgeImports}, 20)
	if err != nil {
		t.Fatalf("get terraform module edges: %v", err)
	}
	if !containsEdge(tfEdges, moduleNetworkID, networkPkgNodeID, EdgeImports) {
		t.Fatalf("expected IMPORTS edge %s -> %s", moduleNetworkID, networkPkgNodeID)
	}
	pkgEdges, err := store.GetOutgoingEdges(ctx, tfPkgNodeID, []EdgeType{EdgeImports}, 20)
	if err != nil {
		t.Fatalf("get terraform package import edges: %v", err)
	}
	if !containsEdge(pkgEdges, tfPkgNodeID, networkPkgNodeID, EdgeImports) {
		t.Fatalf("expected package IMPORTS edge %s -> %s", tfPkgNodeID, networkPkgNodeID)
	}
	refEdges, err := store.GetOutgoingEdges(ctx, localBucketID, []EdgeType{EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get local refs: %v", err)
	}
	if !containsEdge(refEdges, localBucketID, varEnvID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", localBucketID, varEnvID)
	}
	refEdges, err = store.GetOutgoingEdges(ctx, resourceBucketID, []EdgeType{EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get resource refs: %v", err)
	}
	if !containsEdge(refEdges, resourceBucketID, localBucketID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", resourceBucketID, localBucketID)
	}
	refEdges, err = store.GetOutgoingEdges(ctx, outputBucketArnID, []EdgeType{EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get output refs: %v", err)
	}
	if !containsEdge(refEdges, outputBucketArnID, resourceBucketID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", outputBucketArnID, resourceBucketID)
	}
	refEdges, err = store.GetOutgoingEdges(ctx, outputNetworkVpcID, []EdgeType{EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get module output refs: %v", err)
	}
	if !containsEdge(refEdges, outputNetworkVpcID, networkOutputVpcID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", outputNetworkVpcID, networkOutputVpcID)
	}
	refEdges, err = store.GetOutgoingEdges(ctx, moduleNetworkID, []EdgeType{EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get module refs: %v", err)
	}
	if !containsEdge(refEdges, moduleNetworkID, networkVarCIDRID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", moduleNetworkID, networkVarCIDRID)
	}

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

func containsNodeFile(nodes []Node, want string) bool {
	for _, node := range nodes {
		if node.File == want {
			return true
		}
	}
	return false
}
