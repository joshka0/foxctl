import { describe, expect, test } from "bun:test";
import type { ParticipantState } from "@foxctl/data/types";
import { participantTransportKind } from "./room-utils";

describe("room-utils", () => {
  test("participantTransportKind uses explicit transport kind before endpoint shape", () => {
    const transport: ParticipantState = {
      actor_id: "actor:pi:local",
      membership: "active",
      transport_endpoint: "p_21",
      transport_kind: "pi-extension",
      transport: "unknown",
      runtime: "unknown",
      presentation: "none",
      delivery_capability: "viewer_inbox",
      can_trigger_turn: false,
    };

    expect(participantTransportKind(transport)).toBe("pi-extension");
  });
});
