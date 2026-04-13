#!/usr/bin/env bash
set -euo pipefail

CASE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${CASE_DIR}/../../.." && pwd)"

cd "${REPO_ROOT}"

go test -tags=libsqlite3 \
  ./cmd/agentctl/cmd \
  ./internal/storage/coordination \
  ./internal/interfaces/web/api \
  ./internal/interfaces/web/sse \
  -run 'Test(Store_RoomLoopDeliveryRuntimeRoundTrip|RequireActiveRoomLoopRequiresDeliveryOwner|RequireActiveRoomLoopAPIRequiresDeliveryOwner|MergeRelayResultsAllowsLegacyFallbackAfterPrimaryFailure|RoomLoopRuntimeStateFromStoreRestoresOperationalMemory|DetectRoomPulseMessagesSkipsSatisfiedReplyExpected|ProcessRoomReminderTickIgnoresAckedReminderInstance|RoomDetailHandler_GetStatusReturnsPersistedLoopState|RoomDetailHandler_PatchRequiresCoordinator|RoomDetailHandler_MembersPatchRequiresCoordinator|RoomDetailHandler_PutMemberBindingRejectsSelfRoleChange|Handler_GlobalClientReceivesScopedEvent)' \
  -count=1
