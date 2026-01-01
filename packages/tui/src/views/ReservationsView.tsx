// Reservations View - File reservations/locks
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useReservations } from "../hooks/useData";
import type { Reservation } from "@agentctl/data";

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
  const { data: reservations, isLoading, error, refetch } = useReservations();

  const selectedReservation = reservations?.[cursor];

  // Count by mode
  const exclusiveCount = reservations?.filter((r) => r.mode === "exclusive").length || 0;
  const sharedCount = reservations?.filter((r) => r.mode === "shared").length || 0;

  useKeyboard((e) => {
    switch (e.name) {
      case "up":
      case "k":
        setCursor((c) => Math.max(0, c - 1));
        break;
      case "down":
      case "j":
        if (reservations) {
          setCursor((c) => Math.min(reservations.length - 1, c + 1));
        }
        break;
      case "r":
        refetch();
        break;
      case "g":
        setCursor(0);
        break;
      case "G":
        if (reservations) {
          setCursor(reservations.length - 1);
        }
        break;
    }
  });

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
            reservations.map((res, i) => (
              <ReservationRow
                key={res.id}
                reservation={res}
                selected={i === cursor}
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
