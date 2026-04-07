import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { SendHorizonal, X } from 'lucide-react'

interface ReplyComposerProps {
  recipient: string
  subject: string
  onSend: (body: string) => void
  onCancel: () => void
}

export function ReplyComposer({ recipient, subject, onSend, onCancel }: ReplyComposerProps) {
  const [body, setBody] = useState('')

  return (
    <div className="p-3 bg-primary/5 border-y border-primary/10 space-y-3 animate-in slide-in-from-top duration-200">
      <div className="flex items-center justify-between">
        <div className="text-[10px] font-bold text-primary uppercase tracking-tight">
          Reply to {recipient}: {subject}
        </div>
        <Button variant="ghost" size="xs" onClick={onCancel} className="h-5 w-5 p-0">
          <X className="w-3 h-3" />
        </Button>
      </div>
      <Textarea 
        value={body}
        onChange={(e) => setBody(e.target.value)}
        placeholder={`Message for ${recipient}...`}
        className="min-h-[80px] text-xs resize-none bg-background border-primary/20 focus-visible:ring-primary/30"
        autoFocus
      />
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="xs" onClick={onCancel}>Cancel</Button>
        <Button 
          size="xs" 
          disabled={!body.trim()} 
          onClick={() => onSend(body)}
          className="h-7 px-3 text-[10px] font-bold"
        >
          <SendHorizonal className="w-3 h-3 mr-1.5" /> Send Reply
        </Button>
      </div>
    </div>
  )
}
