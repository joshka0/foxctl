import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  useCompanionMemoryStats,
  useCompanionMemoryContext,
  useClearCompanionMemory,
} from "@/api/hooks";
import {
  Brain,
  Calendar,
  History,
  MessageSquare,
  Trash2,
  RefreshCw,
  Loader2,
  AlertCircle,
  ChevronDown,
  ChevronRight,
} from "lucide-react";
import { useState } from "react";

interface MemoryStatsProps {
  conversationId: string | null;
  className?: string;
}

export function MemoryStats({ conversationId, className }: MemoryStatsProps) {
  const [showMemoryContext, setShowMemoryContext] = useState(false);

  const {
    data: stats,
    isLoading,
    error,
    refetch,
  } = useCompanionMemoryStats(conversationId);

  const { data: memoryContext, isLoading: isLoadingContext } =
    useCompanionMemoryContext(showMemoryContext ? conversationId : null);

  const clearMemory = useClearCompanionMemory();

  const handleClearMemory = async () => {
    if (!conversationId) return;
    if (!confirm("Are you sure you want to clear all memory for this conversation?")) {
      return;
    }
    try {
      await clearMemory.mutateAsync(conversationId);
      refetch();
    } catch (err) {
      console.error("Failed to clear memory:", err);
    }
  };

  if (!conversationId) {
    return (
      <Card className={cn("", className)}>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Brain className="h-4 w-4" />
            Memory
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            Select a conversation to view memory stats
          </p>
        </CardContent>
      </Card>
    );
  }

  if (isLoading) {
    return (
      <Card className={cn("", className)}>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Brain className="h-4 w-4" />
            Memory
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin" />
            <span className="text-sm text-muted-foreground">Loading...</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  if (error) {
    return (
      <Card className={cn("", className)}>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Brain className="h-4 w-4" />
            Memory
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2 text-destructive">
            <AlertCircle className="h-4 w-4" />
            <span className="text-sm">Failed to load memory stats</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={cn("", className)}>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Brain className="h-4 w-4" />
            Memory
          </CardTitle>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              className="h-6 w-6"
              onClick={() => refetch()}
              title="Refresh stats"
            >
              <RefreshCw className="h-3 w-3" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-6 w-6 text-destructive hover:text-destructive"
              onClick={handleClearMemory}
              disabled={clearMemory.isPending}
              title="Clear memory"
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Memory tiers */}
        <div className="space-y-3">
          {/* L0: Vivid (Recent turns) */}
          <MemoryTier
            label="L0: Vivid"
            description="Recent conversation turns"
            icon={<MessageSquare className="h-4 w-4" />}
            count={stats?.today_turns ?? 0}
            total={stats?.total_turns ?? 0}
            color="green"
          />

          {/* L1: Day Summaries */}
          <MemoryTier
            label="L1: Summaries"
            description="Daily conversation summaries"
            icon={<Calendar className="h-4 w-4" />}
            count={stats?.day_summaries ?? 0}
            color="blue"
          />

          {/* L2: Distilled History */}
          <MemoryTier
            label="L2: History"
            description="Long-term relationship context"
            icon={<History className="h-4 w-4" />}
            active={stats?.has_history ?? stats?.has_distilled_history ?? false}
            color="purple"
          />
        </div>

        {/* Token usage - only show if backend provides these fields */}
        {stats && (stats.estimated_tokens !== undefined || stats.total_characters !== undefined) && (
          <div className="pt-2 border-t">
            {stats.estimated_tokens !== undefined && (
              <div className="flex items-center justify-between text-xs">
                <span className="text-muted-foreground">Est. tokens</span>
                <span className="font-mono">{stats.estimated_tokens.toLocaleString()}</span>
              </div>
            )}
            {stats.total_characters !== undefined && (
              <div className="flex items-center justify-between text-xs mt-1">
                <span className="text-muted-foreground">Characters</span>
                <span className="font-mono">{stats.total_characters.toLocaleString()}</span>
              </div>
            )}
          </div>
        )}

        {/* Date range */}
        {stats?.oldest_turn && (
          <div className="pt-2 border-t text-xs text-muted-foreground">
            <div className="flex items-center justify-between">
              <span>First message</span>
              <span>{new Date(stats.oldest_turn).toLocaleDateString()}</span>
            </div>
            {stats.newest_turn && (
              <div className="flex items-center justify-between mt-1">
                <span>Last message</span>
                <span>{new Date(stats.newest_turn).toLocaleDateString()}</span>
              </div>
            )}
          </div>
        )}

        {/* Memory context (expandable) */}
        <div className="pt-2 border-t">
          <button
            onClick={() => setShowMemoryContext(!showMemoryContext)}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors w-full"
          >
            {showMemoryContext ? (
              <ChevronDown className="h-3 w-3" />
            ) : (
              <ChevronRight className="h-3 w-3" />
            )}
            View memory context
          </button>

          {showMemoryContext && (
            <div className="mt-2 p-2 bg-muted rounded text-xs font-mono whitespace-pre-wrap max-h-48 overflow-y-auto">
              {isLoadingContext ? (
                <span className="text-muted-foreground">Loading...</span>
              ) : memoryContext?.context ? (
                memoryContext.context
              ) : (
                <span className="text-muted-foreground">No memory context available</span>
              )}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

interface MemoryTierProps {
  label: string;
  description: string;
  icon: React.ReactNode;
  count?: number;
  total?: number;
  active?: boolean;
  color: "green" | "blue" | "purple";
}

function MemoryTier({
  label,
  description,
  icon,
  count,
  total,
  active,
  color,
}: MemoryTierProps) {
  const colorClasses = {
    green: "text-green-600 bg-green-100 dark:bg-green-900/30",
    blue: "text-blue-600 bg-blue-100 dark:bg-blue-900/30",
    purple: "text-purple-600 bg-purple-100 dark:bg-purple-900/30",
  };

  const hasData = (count !== undefined && count > 0) || active;

  return (
    <div className="flex items-start gap-3">
      <div
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-lg",
          hasData ? colorClasses[color] : "bg-muted text-muted-foreground"
        )}
      >
        {icon}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium">{label}</span>
          {count !== undefined && (
            <Badge variant={hasData ? "default" : "outline"} className="text-xs">
              {count}
              {total !== undefined && total !== count && `/${total}`}
            </Badge>
          )}
          {active !== undefined && (
            <Badge
              variant={active ? "success" : "outline"}
              className="text-xs"
            >
              {active ? "Active" : "None"}
            </Badge>
          )}
        </div>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}

export default MemoryStats;
