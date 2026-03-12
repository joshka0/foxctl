package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	tfPkgNodeID := PackageID(repoKey, tfPkgID)
	moduleNetworkID := terraformConceptNodeID(repoKey, tfPkgID, "module:network")
	localBucketID := terraformConceptNodeID(repoKey, tfPkgID, "local:bucket_name")
	varEnvID := terraformConceptNodeID(repoKey, tfPkgID, "variable:env")
	resourceBucketID := terraformConceptNodeID(repoKey, tfPkgID, "resource:aws_s3_bucket.app")
	outputBucketArnID := terraformConceptNodeID(repoKey, tfPkgID, "output:bucket_arn")
	outputNetworkVpcID := terraformConceptNodeID(repoKey, tfPkgID, "output:network_vpc_id")
	networkPkgID := infraPackageIDFromDir(terraformPkgPrefix, "infra/modules/network")
	networkPkgNodeID := PackageID(repoKey, networkPkgID)
	networkVarCIDRID := terraformConceptNodeID(repoKey, networkPkgID, "variable:cidr")
	networkOutputVpcID := terraformConceptNodeID(repoKey, networkPkgID, "output:vpc_id")

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

	refEdges, err = store.GetOutgoingEdges(ctx, moduleNetworkID, []EdgeType{EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get module refs: %v", err)
	}
	if !containsEdge(refEdges, moduleNetworkID, networkVarCIDRID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", moduleNetworkID, networkVarCIDRID)
	}

	refEdges, err = store.GetOutgoingEdges(ctx, outputNetworkVpcID, []EdgeType{EdgeRefersTo}, 20)
	if err != nil {
		t.Fatalf("get module output refs: %v", err)
	}
	if !containsEdge(refEdges, outputNetworkVpcID, networkOutputVpcID, EdgeRefersTo) {
		t.Fatalf("expected REFERS_TO edge %s -> %s", outputNetworkVpcID, networkOutputVpcID)
	}

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

func TestBuilderBuildsHelmChartAndArgoApplicationComponents(t *testing.T) {
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

	writeFile("charts/api/Chart.yaml", `
apiVersion: v2
name: api
version: 0.1.0
description: API chart
`)
	writeFile("charts/api/values.yaml", `
replicaCount: 2
`)
	writeFile("charts/api/templates/deployment.yaml", `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "api.fullname" . }}
`)
	writeFile("charts/api/config/test/values.yaml", `
replicaCount: 1
`)
	writeFile("argocd/api-test.yaml", `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: api-test
  namespace: argocd
spec:
  source:
    path: charts/api
    helm:
      valueFiles:
        - config/test/values.yaml
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
		IncludeKubernetes: true,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.Files < 5 {
		t.Fatalf("files=%d want at least 5", result.Files)
	}

	chartHits, err := store.SearchFTS(ctx, "helm chart api", 10)
	if err != nil {
		t.Fatalf("search helm chart: %v", err)
	}
	if !containsNodeName(chartHits, "chart api") {
		t.Fatalf("expected helm chart concept in %#v", chartHits)
	}

	appHits, err := store.SearchFTS(ctx, "argocd application api test", 10)
	if err != nil {
		t.Fatalf("search argocd application: %v", err)
	}
	if !containsNodeName(appHits, "application argocd/api-test") {
		t.Fatalf("expected argocd application concept in %#v", appHits)
	}

	k8sHits, err := store.SearchFTS(ctx, "Deployment api", 10)
	if err != nil {
		t.Fatalf("search template deployment: %v", err)
	}
	if !containsNodeFile(k8sHits, "charts/api/templates/deployment.yaml") {
		t.Fatalf("expected templated deployment concept in %#v", k8sHits)
	}

	repoKey := store.RepoKey()
	chartPkgID := infraPackageID(kubernetesPkgPrefix, "charts/api/Chart.yaml")
	chartNodeID := NamespacedID(repoKey, ConceptChart+chartPkgID+":api")
	appNodeID := NamespacedID(repoKey, ConceptApp+"argocd/api-test.yaml:"+strings.ToLower("api-test"))
	valueFileID := FileID(repoKey, infraPackageID(kubernetesPkgPrefix, "charts/api/config/test/values.yaml"), "charts/api/config/test/values.yaml")
	chartValueFileID := FileID(repoKey, infraPackageID(kubernetesPkgPrefix, "charts/api/values.yaml"), "charts/api/values.yaml")

	outgoing, err := store.GetOutgoingEdges(ctx, chartNodeID, []EdgeType{EdgeContains}, 50)
	if err != nil {
		t.Fatalf("get chart outgoing edges: %v", err)
	}
	if !containsEdge(outgoing, chartNodeID, chartValueFileID, EdgeContains) {
		t.Fatalf("expected chart contains values.yaml")
	}

	outgoing, err = store.GetOutgoingEdges(ctx, appNodeID, []EdgeType{EdgeImports, EdgeRefersTo}, 50)
	if err != nil {
		t.Fatalf("get app outgoing edges: %v", err)
	}
	if !containsEdge(outgoing, appNodeID, chartNodeID, EdgeImports) {
		t.Fatalf("expected application imports chart")
	}
	if !containsEdge(outgoing, appNodeID, valueFileID, EdgeRefersTo) {
		t.Fatalf("expected application refers to values file")
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
