import { useEffect, useMemo, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import {
  buildRoomTerminalWebSocketURL,
  decodeTerminalControlText,
  decodeTerminalOutput,
  encodeResizeControl,
  encodeTerminalInput,
} from '@/lib/room-terminal-protocol'
import { Link, SendHorizonal, Unplug } from 'lucide-react'

interface RoomLiveTerminalPanelProps {
  roomId: string
  roomLabel?: string
}

type ConnectionState = 'idle' | 'connecting' | 'connected' | 'closed' | 'error'

const DEFAULT_COLS = 120
const DEFAULT_ROWS = 36
const MAX_OUTPUT_CHARS = 80_000

export function RoomLiveTerminalPanel({ roomId, roomLabel }: RoomLiveTerminalPanelProps) {
  const wsRef = useRef<WebSocket | null>(null)
  const outputRef = useRef<HTMLSpanElement | null>(null)
  const [connectionState, setConnectionState] = useState<ConnectionState>('idle')
  const [output, setOutput] = useState('')
  const [draft, setDraft] = useState('')
  const [protocolStatus, setProtocolStatus] = useState('')
  const [cols, setCols] = useState(String(DEFAULT_COLS))
  const [rows, setRows] = useState(String(DEFAULT_ROWS))

  const wsUrl = useMemo(() => {
    if (typeof window === 'undefined') return ''
    return buildRoomTerminalWebSocketURL(window.location, roomId, parseDimension(cols, DEFAULT_COLS), parseDimension(rows, DEFAULT_ROWS))
  }, [cols, roomId, rows])

  useEffect(() => {
    return () => {
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [])

  useEffect(() => {
    outputRef.current?.scrollIntoView({ block: 'end' })
  }, [output])

  const connected = connectionState === 'connected'

  function appendOutput(next: string) {
    setOutput((current) => (current + next).slice(-MAX_OUTPUT_CHARS))
  }

  function closeSocket() {
    wsRef.current?.close()
    wsRef.current = null
    setConnectionState('closed')
  }

  function connectSocket() {
    if (!wsUrl) return
    closeSocket()
    setConnectionState('connecting')
    const ws = new WebSocket(wsUrl)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      setConnectionState('connected')
      setProtocolStatus('')
      ws.send(encodeResizeControl(parseDimension(cols, DEFAULT_COLS), parseDimension(rows, DEFAULT_ROWS)))
    }
    ws.onmessage = (event) => {
      if (typeof event.data === 'string') {
        setProtocolStatus(decodeTerminalControlText(event.data) || '')
        return
      }
      void decodeTerminalOutput(event.data).then((text) => appendOutput(text))
    }
    ws.onerror = () => setConnectionState('error')
    ws.onclose = () => {
      if (wsRef.current === ws) {
        wsRef.current = null
        setConnectionState((current) => (current === 'error' ? 'error' : 'closed'))
      }
    }
  }

  function sendInput(input: string) {
    const ws = wsRef.current
    if (!ws || ws.readyState !== WebSocket.OPEN || !input) return
    ws.send(encodeTerminalInput(input))
  }

  function sendResize() {
    const ws = wsRef.current
    if (!ws || ws.readyState !== WebSocket.OPEN) return
    ws.send(encodeResizeControl(parseDimension(cols, DEFAULT_COLS), parseDimension(rows, DEFAULT_ROWS)))
  }

  return (
    <div className="mx-4 mb-4 rounded-md border border-amber-500/40 bg-amber-500/5">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-amber-500/20 px-3 py-2">
        <div className="min-w-0">
          <div className="text-[10px] font-black uppercase tracking-widest text-amber-700">Experimental Live Terminal</div>
          {roomLabel && roomLabel !== roomId ? (
            <div className="truncate font-mono text-[10px] text-muted-foreground">
              room:{roomLabel}
            </div>
          ) : null}
          <div className="truncate font-mono text-[10px] text-muted-foreground">
            /ws/terminal/{roomId}
          </div>
          {protocolStatus ? (
            <div className="truncate font-mono text-[10px] text-red-700">
              {protocolStatus}
            </div>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <span className={cn('font-mono text-[10px]', connected ? 'text-green-700' : connectionState === 'error' ? 'text-red-700' : 'text-muted-foreground')}>
            {connectionState}
          </span>
          <Button variant="outline" size="xs" onClick={connected ? closeSocket : connectSocket}>
            {connected ? <Unplug className="h-3 w-3" /> : <Link className="h-3 w-3" />}
            {connected ? 'Disconnect' : 'Connect'}
          </Button>
        </div>
      </div>
      <ScrollArea className="h-52 bg-[#101010]">
        <pre className="min-h-52 whitespace-pre-wrap break-words px-3 py-2 font-mono text-[11px] leading-5 text-[#e6e6e6]">
          {output || 'Connect to dogfood the room PTY bridge.'}
          <span ref={outputRef} />
        </pre>
      </ScrollArea>
      <div className="grid gap-2 border-t border-amber-500/20 p-3 md:grid-cols-[72px_72px_minmax(0,1fr)_auto_auto]">
        <Input value={cols} onChange={(event) => setCols(event.target.value)} className="h-8 font-mono text-xs" aria-label="Terminal columns" />
        <Input value={rows} onChange={(event) => setRows(event.target.value)} className="h-8 font-mono text-xs" aria-label="Terminal rows" />
        <Input
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== 'Enter') return
            event.preventDefault()
            sendInput(`${draft}\n`)
            setDraft('')
          }}
          placeholder="PTY input"
          className="h-8 font-mono text-xs"
          disabled={!connected}
        />
        <Button variant="outline" size="sm" className="h-8 text-[10px] font-black uppercase tracking-tight" disabled={!connected} onClick={sendResize}>
          Resize
        </Button>
        <Button
          size="sm"
          className="h-8 text-[10px] font-black uppercase tracking-tight"
          disabled={!connected || !draft}
          onClick={() => {
            sendInput(`${draft}\n`)
            setDraft('')
          }}
        >
          <SendHorizonal className="h-3 w-3" />
          Send
        </Button>
      </div>
    </div>
  )
}

function parseDimension(value: string, fallback: number): number {
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : fallback
}
