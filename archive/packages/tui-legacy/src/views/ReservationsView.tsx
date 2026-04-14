// Reservations View - File reservations/locks
import { useEffect, useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useReservations } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { Reservation } from "@foxctl/data";

function modeColor(mode: string): string {
  return mode === "exclusive" ? "#ff0000" : "#00ff00";
}

interface ReservationRowProps {
  reservation: Reservation;
  selected: boolean;
}

function ReservationRow({ reservation, selected }: ReservationRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  const pathDisplay =
    reservation.path.length > 45
      ? "..." + reservation.path.slice(-42)
      : reservation.path;

  return (
    <box height={2} backgroundColor={bg} flexDirection="column">
      <text fg="#ffffff">
        {cursor}
        <span fg={modeColor(reservation.mode)}>
          [{reservation.mode === "exclusive" ? "X" : "S"}]
        </span>
        {"  "}
        {pathDisplay}
      </text>
      <text fg="#666666">
        {"     "}Holder: {reservation.holder.slice(0, 20)}
        {"  |  "}Expires: {reservation.expires_at.slice(11, 19)}
      </text>
    </box>
  );
}

interface ReservationDetailProps {
  reservation: Reservation | undefined;
}

function ReservationDetail({ reservation }: ReservationDetailProps) {
  if (!reservation) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a reservation to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <text fg="#aa77ff">
        <b>Reservation Detail</b>
      </text>
      <text> </text>
      <text>
        <b fg="#666666">ID: </b>
        <span fg="#ffffff">{reservation.id}</span>
      </text>
      <text>
        <b fg="#666666">Path: </b>
        <span fg="#ffffff">{reservation.path}</span>
      </text>
      <text>
        <b fg="#666666">Holder: </b>
        <span fg="#00ffff">{reservation.holder}</span>
      </text>
      <text>
        <b fg="#666666">Mode: </b>
        <span fg={modeColor(reservation.mode)}>
          {reservation.mode.toUpperCase()}
        </span>
      </text>
      <text>
        <b fg="#666666">Expires: </b>
        <span fg="#ffffff">{reservation.expires_at}</span>
      </text>
      <text> </text>
      <text fg="#666666">
        {reservation.mode === "exclusive"
          ? "This file is exclusively locked - no other agents can modify it."
          : "This file has a shared lock - other agents can read but not modify."}
      </text>
    </box>
  );
}

export function ReservationsView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const { data: reservations, isLoading, error, refetch } = useReservations();

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT; // Number of visible rows (each row is 2 lines high, so 16 lines total)

  const selectedReservation = reservations?.[cursor];

  // Count by mode
  const exclusiveCount = reservations?.filter((r) => r.mode === "exclusive").length || 0;
  const sharedCount = reservations?.filter((r) => r.mode === "shared").length || 0;

  const updateCursor = (newCursor: number) => {
    if (!reservations) return;
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (!reservations) return;
    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(reservations.length - 1, cursor + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(reservations.length - 1);
        break;
    }
  });

  useEffect(() => {
    if (!reservations) return;
    const maxCursor = Math.max(0, reservations.length - 1);
    const nextCursor = Math.min(cursor, maxCursor);
    setCursor(nextCursor);
    setScrollOffset((s) => {
      const maxScroll = Math.max(0, reservations.length - LIST_HEIGHT);
      const clampedScroll = Math.min(s, maxScroll);
      if (nextCursor < clampedScroll) return nextCursor;
      if (nextCursor >= clampedScroll + LIST_HEIGHT) {
        return Math.max(0, nextCursor - LIST_HEIGHT + 1);
      }
      return clampedScroll;
    });
  }, [cursor, reservations?.length]);

  if (isLoading && !reservations) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading reservations...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading reservations: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Reservation list */}
      <box width="55%" flexDirection="column">
        <box height={2} paddingLeft={1}>
          <text fg="#aa77ff">
            <b>File Reservations</b>
            <span fg="#666666"> ({reservations?.length || 0} active)</span>
          </text>
          <text fg="#666666">
            {"  "}
            <span fg="#ff0000">[X]</span> Exclusive: {exclusiveCount}
            {"  "}
            <span fg="#00ff00">[S]</span> Shared: {sharedCount}
          </text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden" paddingLeft={1}>
          {!reservations || reservations.length === 0 ? (
            <box padding={1}>
              <text fg="#888888">No active reservations</text>
            </box>
          ) : (
            reservations.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((res, i) => (
              <ReservationRow
                key={res.id}
                reservation={res}
                selected={i + scrollOffset === cursor}
              />
            ))
          )}
        </box>
      </box>

      {/* Detail pane */}
      <box
        flexGrow={1}
        borderStyle="single"
        borderColor="#444444"
        border={["left"]}
      >
        <ReservationDetail reservation={selectedReservation} />
      </box>
    </box>
  );
}
