export interface RoomTerminalLocation {
  protocol: string
  host: string
  hostname: string
  search: string
  hash?: string
}

export interface RoomTerminalStorage {
  getItem(key: string): string | null
}

export interface ResizeControl {
  type: 'resize'
  cols: number
  rows: number
}

const DOGFOOD_PARAM = 'foxctl_live_terminal'
const DOGFOOD_STORAGE_KEY = 'foxctl.liveTerminalDogfood'

/**
 * Gates the PR-C browser-terminal dogfood surface to explicit local/tailnet use.
 *
 * Index:
 *   Purpose: Keeps the live GUI terminal on the room-terminal compatibility endpoint.
 *   Keywords: remote workbench PR-C, room terminal dogfood, live terminal, tailnet
 *   Related: RoomLiveTerminalPanel, buildRoomTerminalWebSocketURL
 */
export function isRoomLiveTerminalEnabled(location: RoomTerminalLocation, storage?: RoomTerminalStorage): boolean {
  if (!isLocalOrTailnetHost(location.hostname)) return false

  const params = roomTerminalSearchParams(location)
  const paramValue = params.get(DOGFOOD_PARAM)
  if (isTruthyFlag(paramValue)) return true
  if (isFalsyFlag(paramValue)) return false

  return isTruthyFlag(storage?.getItem(DOGFOOD_STORAGE_KEY) ?? null)
}

export function isLocalOrTailnetHost(hostname: string): boolean {
  const host = hostname.trim().toLowerCase().replace(/^\[/, '').replace(/\]$/, '')
  if (host === 'localhost' || host === '127.0.0.1' || host === '0.0.0.0' || host === '::1') return true
  if (host.endsWith('.ts.net')) return true
  return isTailscaleIPv4(host)
}

export function buildRoomTerminalWebSocketURL(location: RoomTerminalLocation, roomId: string, cols: number, rows: number): string {
  const wsProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const params = new URLSearchParams({
    cols: String(clampTerminalDimension(cols)),
    rows: String(clampTerminalDimension(rows)),
  })
  return `${wsProtocol}//${location.host}/ws/terminal/${encodeURIComponent(roomId)}?${params.toString()}`
}

export function encodeTerminalInput(input: string): Uint8Array {
  return new TextEncoder().encode(input)
}

export function encodeResizeControl(cols: number, rows: number): string {
  return JSON.stringify({
    type: 'resize',
    cols: clampTerminalDimension(cols),
    rows: clampTerminalDimension(rows),
  } satisfies ResizeControl)
}

export function decodeTerminalOutput(data: string | ArrayBuffer | Blob): Promise<string> {
  if (typeof data === 'string') return Promise.resolve(data)
  if (data instanceof Blob) return data.text()
  return Promise.resolve(new TextDecoder().decode(new Uint8Array(data)))
}

export function decodeTerminalControlText(data: string): string | null {
  try {
    const parsed = JSON.parse(data) as { type?: unknown; message?: unknown }
    if (parsed.type === 'error') {
      return typeof parsed.message === 'string' ? parsed.message : 'terminal protocol error'
    }
    return null
  } catch {
    return null
  }
}

function clampTerminalDimension(value: number): number {
  if (!Number.isFinite(value)) return 80
  return Math.max(1, Math.min(1000, Math.floor(value)))
}

function isTruthyFlag(value: string | null): boolean {
  return value === '1' || value === 'true' || value === 'yes'
}

function isFalsyFlag(value: string | null): boolean {
  return value === '0' || value === 'false' || value === 'no'
}

function isTailscaleIPv4(host: string): boolean {
  const parts = host.split('.').map((part) => Number.parseInt(part, 10))
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) return false
  return parts[0] === 100 && parts[1] >= 64 && parts[1] <= 127
}

function roomTerminalSearchParams(location: RoomTerminalLocation): URLSearchParams {
  const params = new URLSearchParams(location.search)
  const hashQuery = location.hash?.split('?')[1]
  if (hashQuery) {
    for (const [key, value] of new URLSearchParams(hashQuery)) {
      if (!params.has(key)) params.set(key, value)
    }
  }
  return params
}
