#!/usr/bin/env bash
set -euo pipefail

CASE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${CASE_DIR}/../../.." && pwd)"

cd "${REPO_ROOT}"

CGO_ENABLED=0 go test \
  ./skills/hooks_task_guard \
  ./internal/context/contextplane \
  ./internal/interfaces/web/api \
  -run 'Test(TaskGuard_ProposalMode_(NoActiveTask_RecordsProposalNoTask|DedupeEquivalentEvents|DedupesProviderAliases|WithActiveTask_DirtiesAndNoProposal|BlocksUnsafeScope|RequiresExplicitWorkspaceContext)|RecordControlProposalDedupesByDedupeKey|CoordinatorDecisionsAreAppendOnly|RecordApplyResultIsIdempotentByKey|RejectsInvalidControlTransitions|ListControlProposalStatesDerivesLatestState|ApplyMemoryCandidate(RequiresApprovalDecision|AppliesOneNamedMemoryIdempotently|RejectDecisionCannotApply)|ContextControlProposalsListFiltersDerivedStatus|ContextControlProposalDecision(AppendsAndUpdatesLatestState|PreservesEvidenceAndHarnessMetadata|RequiresEvidenceRefs|RejectsInvalidEvidenceRefs|DoesNotCreateApplyResults))$' \
  -count=1

if rg -n 'func Test.*Coordinator' cmd/foxctl/cmd --glob '*_test.go' >/dev/null 2>&1; then
  CGO_ENABLED=0 go test ./cmd/foxctl/cmd -run 'Test.*Coordinator' -count=1
fi
