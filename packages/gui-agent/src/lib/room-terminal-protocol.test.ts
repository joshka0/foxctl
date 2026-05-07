import { describe, expect, test } from "bun:test";
import {
  buildRoomTerminalWebSocketURL,
  decodeTerminalControlText,
  encodeResizeControl,
  encodeTerminalInput,
  isLocalOrTailnetHost,
  isRoomLiveTerminalEnabled,
} from "./room-terminal-protocol";

describe("room-terminal-protocol", () => {
  test("gates the live terminal to local and tailnet hosts", () => {
    expect(isLocalOrTailnetHost("localhost")).toBe(true);
    expect(isLocalOrTailnetHost("127.0.0.1")).toBe(true);
    expect(isLocalOrTailnetHost("[::1]")).toBe(true);
    expect(isLocalOrTailnetHost("dev.tail0000.ts.net")).toBe(true);
    expect(isLocalOrTailnetHost("100.90.10.1")).toBe(true);
    expect(isLocalOrTailnetHost("example.com")).toBe(false);
  });

  test("requires an explicit dogfood flag", () => {
    const local = { protocol: "http:", host: "localhost:5173", hostname: "localhost", search: "" };
    const publicHost = { protocol: "https:", host: "example.com", hostname: "example.com", search: "?foxctl_live_terminal=1" };

    expect(isRoomLiveTerminalEnabled(local)).toBe(false);
    expect(isRoomLiveTerminalEnabled({ ...local, search: "?foxctl_live_terminal=1" })).toBe(true);
    expect(isRoomLiveTerminalEnabled({ ...local, hash: "#rooms?foxctl_live_terminal=1" })).toBe(true);
    expect(isRoomLiveTerminalEnabled({ ...local, search: "?foxctl_live_terminal=0" }, { getItem: () => "1" })).toBe(false);
    expect(isRoomLiveTerminalEnabled(local, { getItem: () => "true" })).toBe(true);
    expect(isRoomLiveTerminalEnabled(publicHost)).toBe(false);
  });

  test("builds the compatibility room terminal websocket URL", () => {
    expect(
      buildRoomTerminalWebSocketURL(
        { protocol: "https:", host: "dev.ts.net", hostname: "dev.ts.net", search: "" },
        "room/with spaces",
        120,
        40,
      ),
    ).toBe("wss://dev.ts.net/ws/terminal/room%2Fwith%20spaces?cols=120&rows=40");
  });

  test("encodes terminal input as binary bytes", () => {
    expect(Array.from(encodeTerminalInput("echo ok\n"))).toEqual([101, 99, 104, 111, 32, 111, 107, 10]);
  });

  test("encodes resize as a JSON text control", () => {
    expect(encodeResizeControl(120.8, 40)).toBe('{"type":"resize","cols":120,"rows":40}');
    expect(encodeResizeControl(2000, 0)).toBe('{"type":"resize","cols":1000,"rows":1}');
  });

  test("decodes server text frames as terminal controls", () => {
    expect(decodeTerminalControlText('{"type":"error","message":"bad control"}')).toBe("bad control");
    expect(decodeTerminalControlText('{"type":"resize","cols":120,"rows":40}')).toBeNull();
    expect(decodeTerminalControlText("plain text")).toBeNull();
  });
});
