import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, formatRelativeTime } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Card, CardContent } from '@/components/ui/card'
import { Mail, Clipboard, RefreshCw, AlertCircle } from 'lucide-react'
import { listMailbox, listBlackboard } from '@/api/client'
import type { MailboxMessage, BlackboardRecord } from '@/api/types'

type TabType = 'mailbox' | 'blackboard'

// Default workspace - in production this would come from context
const DEFAULT_WORKSPACE = '.'

export function CommunicationPanel() {
  const [activeTab, setActiveTab] = useState<TabType>('mailbox')

  const mailboxQuery = useQuery({
    queryKey: ['mailbox'],
    queryFn: () => listMailbox({ workspace_id: DEFAULT_WORKSPACE, limit: 20 }),
    refetchInterval: 10000,
  })

  const blackboardQuery = useQuery({
    queryKey: ['blackboard'],
    queryFn: () => listBlackboard({ limit: 20 }),
    refetchInterval: 10000,
  })

  const mailboxCount = mailboxQuery.data?.messages?.length ?? 0
  const blackboardCount = blackboardQuery.data?.records?.length ?? 0

  return (
    <div className="flex flex-col h-full">
      {/* Tabs */}
      <div className="flex border-b border-border">
        <TabButton
          active={activeTab === 'mailbox'}
          onClick={() => setActiveTab('mailbox')}
          icon={<Mail className="h-4 w-4" />}
          label="Mailbox"
          badge={mailboxCount}
        />
        <TabButton
          active={activeTab === 'blackboard'}
          onClick={() => setActiveTab('blackboard')}
          icon={<Clipboard className="h-4 w-4" />}
          label="Blackboard"
          badge={blackboardCount}
        />
      </div>

      {/* Content */}
      <ScrollArea className="flex-1 p-4">
        {activeTab === 'mailbox' ? (
          <MailboxContent
            messages={mailboxQuery.data?.messages ?? []}
            isLoading={mailboxQuery.isLoading}
            refetch={mailboxQuery.refetch}
            isFetching={mailboxQuery.isFetching}
          />
        ) : (
          <BlackboardContent
            records={blackboardQuery.data?.records ?? []}
            isLoading={blackboardQuery.isLoading}
            refetch={blackboardQuery.refetch}
            isFetching={blackboardQuery.isFetching}
          />
        )}
      </ScrollArea>
    </div>
  )
}

interface TabButtonProps {
  active: boolean
  onClick: () => void
  icon: React.ReactNode
  label: string
  badge?: number
}

function TabButton({ active, onClick, icon, label, badge }: TabButtonProps) {
  return (
    <button
      className={cn(
        'flex-1 flex items-center justify-center gap-2 py-2 text-sm font-medium transition-colors border-b-2',
        active
          ? 'border-primary text-foreground'
          : 'border-transparent text-muted-foreground hover:text-foreground'
      )}
      onClick={onClick}
    >
      {icon}
      <span>{label}</span>
      {badge !== undefined && badge > 0 && (
        <Badge variant="secondary" className="h-5 min-w-5 px-1">
          {badge}
        </Badge>
      )}
    </button>
  )
}

interface MailboxContentProps {
  messages: MailboxMessage[]
  isLoading: boolean
  refetch: () => void
  isFetching: boolean
}

function MailboxContent({ messages, isLoading, refetch, isFetching }: MailboxContentProps) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-foreground">Messages</span>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6"
          onClick={() => refetch()}
          disabled={isFetching}
          aria-label="Refresh mailbox"
        >
          <RefreshCw className={cn('h-3 w-3', isFetching && 'animate-spin')} />
        </Button>
      </div>
      {isLoading ? (
        <div className="text-center py-8">
          <RefreshCw className="h-5 w-5 mx-auto animate-spin text-muted-foreground" />
        </div>
      ) : messages.length === 0 ? (
        <div className="text-center py-8">
          <Mail className="h-8 w-8 mx-auto mb-2 text-muted-foreground opacity-50" />
          <p className="text-sm text-muted-foreground">No messages</p>
        </div>
      ) : (
        messages.map((msg) => (
          <Card key={msg.id} className="cursor-pointer hover:bg-accent/50">
            <CardContent className="p-3">
              <div className="flex items-start justify-between mb-1">
                <span className="text-sm font-medium text-foreground">
                  {msg.sender}
                </span>
                <span className="text-xs text-muted-foreground">
                  {formatRelativeTime(msg.created_at)}
                </span>
              </div>
              <p className="text-sm font-medium text-foreground truncate">
                {msg.subject}
              </p>
              <p className="text-xs text-muted-foreground truncate mt-1">
                {msg.body.slice(0, 100)}
              </p>
              <div className="flex items-center gap-2 mt-2">
                {msg.priority === 1 && (
                  <Badge variant="destructive">Urgent</Badge>
                )}
                {msg.status === 'unread' && (
                  <Badge variant="secondary">Unread</Badge>
                )}
                <Badge variant="outline" className="text-xs">
                  {msg.kind}
                </Badge>
              </div>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  )
}

interface BlackboardContentProps {
  records: BlackboardRecord[]
  isLoading: boolean
  refetch: () => void
  isFetching: boolean
}

function BlackboardContent({ records, isLoading, refetch, isFetching }: BlackboardContentProps) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-foreground">Records</span>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6"
          onClick={() => refetch()}
          disabled={isFetching}
          aria-label="Refresh blackboard"
        >
          <RefreshCw className={cn('h-3 w-3', isFetching && 'animate-spin')} />
        </Button>
      </div>
      {isLoading ? (
        <div className="text-center py-8">
          <RefreshCw className="h-5 w-5 mx-auto animate-spin text-muted-foreground" />
        </div>
      ) : records.length === 0 ? (
        <div className="text-center py-8">
          <Clipboard className="h-8 w-8 mx-auto mb-2 text-muted-foreground opacity-50" />
          <p className="text-sm text-muted-foreground">No records</p>
        </div>
      ) : (
        records.map((rec) => (
          <Card key={rec.id} className="cursor-pointer hover:bg-accent/50">
            <CardContent className="p-3">
              <div className="flex items-start justify-between mb-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium text-foreground">
                    {rec.topic}
                  </span>
                  <Badge variant="outline" className="text-xs">
                    {rec.ns}
                  </Badge>
                </div>
                <span className="text-xs text-muted-foreground">
                  {formatRelativeTime(new Date(rec.ts * 1000).toISOString())}
                </span>
              </div>
              <p className="text-sm text-muted-foreground truncate">
                {rec.payload.slice(0, 100)}
              </p>
              {rec.ttl_sec > 0 && (
                <div className="flex items-center gap-1 mt-2 text-xs text-muted-foreground">
                  <AlertCircle className="h-3 w-3" />
                  TTL: {Math.round(rec.ttl_sec / 60)}m
                </div>
              )}
            </CardContent>
          </Card>
        ))
      )}
    </div>
  )
}
