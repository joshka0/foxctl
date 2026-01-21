import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import {
  useCompanionContext,
  useSetCompanionContext,
  useDeleteCompanionContext,
  useClearCompanionContext,
} from "@/api/hooks";
import type { CompanionContextVariable } from "@/api/client";
import {
  Database,
  Plus,
  Trash2,
  RefreshCw,
  Loader2,
  AlertCircle,
  ChevronDown,
  ChevronRight,
  Globe,
  MessageCircle,
  Zap,
  Clock,
} from "lucide-react";

interface ContextViewerProps {
  conversationId: string | null;
  className?: string;
}

export function ContextViewer({ conversationId, className }: ContextViewerProps) {
  const [expandedVars, setExpandedVars] = useState<Set<string>>(new Set());
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [newVarKey, setNewVarKey] = useState("");
  const [newVarValue, setNewVarValue] = useState("");
  const [newVarScope, setNewVarScope] = useState<"global" | "conversation" | "turn">("conversation");

  const {
    data: contextData,
    isLoading,
    error,
    refetch,
  } = useCompanionContext(conversationId);

  const setContext = useSetCompanionContext();
  const deleteContext = useDeleteCompanionContext();
  const clearContext = useClearCompanionContext();

  const toggleExpanded = (id: string) => {
    setExpandedVars((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleAddVariable = async () => {
    if (!conversationId || !newVarKey.trim()) return;

    try {
      let parsedValue: unknown = newVarValue;
      try {
        parsedValue = JSON.parse(newVarValue);
      } catch {
        // Keep as string if not valid JSON
      }

      await setContext.mutateAsync({
        conversation_id: conversationId,
        key: newVarKey.trim(),
        value: parsedValue,
        scope: newVarScope,
      });

      setNewVarKey("");
      setNewVarValue("");
      setShowAddDialog(false);
      refetch();
    } catch (err) {
      console.error("Failed to add context variable:", err);
    }
  };

  const handleDeleteVariable = async (key: string, scope: string) => {
    if (!conversationId) return;
    if (!confirm(`Delete context variable "${key}"?`)) return;

    try {
      await deleteContext.mutateAsync({
        conversationId,
        key,
        scope,
      });
      refetch();
    } catch (err) {
      console.error("Failed to delete context variable:", err);
    }
  };

  const handleClearAll = async () => {
    if (!conversationId) return;
    if (!confirm("Clear all context variables for this conversation?")) return;

    try {
      await clearContext.mutateAsync(conversationId);
      refetch();
    } catch (err) {
      console.error("Failed to clear context:", err);
    }
  };

  // Group variables by scope
  const groupedVars = (contextData?.variables ?? []).reduce<
    Record<string, CompanionContextVariable[]>
  >((acc, v) => {
    const scope = v.scope || "conversation";
    if (!acc[scope]) acc[scope] = [];
    acc[scope].push(v);
    return acc;
  }, {});

  const scopeOrder = ["global", "conversation", "turn"];
  const sortedScopes = Object.keys(groupedVars).sort(
    (a, b) => scopeOrder.indexOf(a) - scopeOrder.indexOf(b)
  );

  if (!conversationId) {
    return (
      <Card className={cn("", className)}>
        <CardHeader className="pb-3">
          <CardTitle className="text-sm font-medium flex items-center gap-2">
            <Database className="h-4 w-4" />
            Context Variables
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">
            Select a conversation to view context
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
            <Database className="h-4 w-4" />
            Context Variables
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
            <Database className="h-4 w-4" />
            Context Variables
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2 text-destructive">
            <AlertCircle className="h-4 w-4" />
            <span className="text-sm">Failed to load context</span>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <>
      <Card className={cn("", className)}>
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm font-medium flex items-center gap-2">
              <Database className="h-4 w-4" />
              Context
              {contextData && contextData.total_count > 0 && (
                <Badge variant="secondary" className="text-xs">
                  {contextData.total_count}
                </Badge>
              )}
            </CardTitle>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => setShowAddDialog(true)}
                title="Add variable"
              >
                <Plus className="h-3 w-3" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6"
                onClick={() => refetch()}
                title="Refresh"
              >
                <RefreshCw className="h-3 w-3" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6 text-destructive hover:text-destructive"
                onClick={handleClearAll}
                disabled={clearContext.isPending}
                title="Clear all"
              >
                <Trash2 className="h-3 w-3" />
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {sortedScopes.length === 0 ? (
            <p className="text-sm text-muted-foreground">No context variables</p>
          ) : (
            <div className="space-y-4">
              {sortedScopes.map((scope) => (
                <ScopeSection
                  key={scope}
                  scope={scope}
                  variables={groupedVars[scope]}
                  expandedVars={expandedVars}
                  onToggleExpanded={toggleExpanded}
                  onDelete={handleDeleteVariable}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Add Variable Dialog */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Context Variable</DialogTitle>
            <DialogDescription>
              Add a new context variable that the companion can use.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label htmlFor="newVarKey" className="text-sm font-medium">Key</label>
              <Input
                id="newVarKey"
                value={newVarKey}
                onChange={(e) => setNewVarKey(e.target.value)}
                placeholder="e.g., user_preference"
              />
            </div>

            <div className="space-y-2">
              <label htmlFor="newVarValue" className="text-sm font-medium">Value (JSON or string)</label>
              <Input
                id="newVarValue"
                value={newVarValue}
                onChange={(e) => setNewVarValue(e.target.value)}
                placeholder='e.g., "dark_mode" or {"theme": "dark"}'
              />
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">Scope</label>
              <div className="flex gap-2">
                {(["global", "conversation", "turn"] as const).map((scope) => (
                  <Button
                    key={scope}
                    variant={newVarScope === scope ? "default" : "outline"}
                    size="sm"
                    onClick={() => setNewVarScope(scope)}
                  >
                    {scope}
                  </Button>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                {newVarScope === "global" && "Persists across all conversations"}
                {newVarScope === "conversation" && "Persists within this conversation"}
                {newVarScope === "turn" && "Only available for the current turn"}
              </p>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleAddVariable}
              disabled={!newVarKey.trim() || setContext.isPending}
            >
              {setContext.isPending ? (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              ) : (
                <Plus className="h-4 w-4 mr-2" />
              )}
              Add Variable
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

interface ScopeSectionProps {
  scope: string;
  variables: CompanionContextVariable[];
  expandedVars: Set<string>;
  onToggleExpanded: (id: string) => void;
  onDelete: (key: string, scope: string) => void;
}

function ScopeSection({
  scope,
  variables,
  expandedVars,
  onToggleExpanded,
  onDelete,
}: ScopeSectionProps) {
  const scopeConfig = {
    global: { icon: Globe, color: "text-purple-600", label: "Global" },
    conversation: { icon: MessageCircle, color: "text-blue-600", label: "Conversation" },
    turn: { icon: Zap, color: "text-green-600", label: "Turn" },
  }[scope] ?? { icon: Database, color: "text-gray-600", label: scope };

  const ScopeIcon = scopeConfig.icon;

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <ScopeIcon className={cn("h-3 w-3", scopeConfig.color)} />
        <span className="text-xs font-medium text-muted-foreground uppercase">
          {scopeConfig.label}
        </span>
        <Badge variant="outline" className="text-xs h-4">
          {variables.length}
        </Badge>
      </div>

      <div className="space-y-1 pl-5">
        {variables.map((v) => (
          <ContextVariable
            key={v.id}
            variable={v}
            isExpanded={expandedVars.has(v.id)}
            onToggle={() => onToggleExpanded(v.id)}
            onDelete={() => onDelete(v.key, v.scope)}
          />
        ))}
      </div>
    </div>
  );
}

interface ContextVariableProps {
  variable: CompanionContextVariable;
  isExpanded: boolean;
  onToggle: () => void;
  onDelete: () => void;
}

function ContextVariable({
  variable,
  isExpanded,
  onToggle,
  onDelete,
}: ContextVariableProps) {
  const valuePreview =
    typeof variable.value === "string"
      ? variable.value.length > 50
        ? `"${variable.value.slice(0, 50)}..."`
        : `"${variable.value}"`
      : JSON.stringify(variable.value).slice(0, 50);

  return (
    <div className="group">
      <div className="flex items-center gap-2">
        <button
          onClick={onToggle}
          className="flex items-center gap-1 text-xs hover:text-foreground transition-colors"
        >
          {isExpanded ? (
            <ChevronDown className="h-3 w-3" />
          ) : (
            <ChevronRight className="h-3 w-3" />
          )}
          <span className="font-medium">{variable.key}</span>
        </button>

        {!isExpanded && (
          <span className="text-xs text-muted-foreground truncate flex-1 font-mono">
            {valuePreview}
          </span>
        )}

        {variable.access_count > 0 && (
          <Badge variant="outline" className="text-xs h-4 gap-1">
            <Clock className="h-2 w-2" />
            {variable.access_count}
          </Badge>
        )}

        <Button
          variant="ghost"
          size="icon"
          className="h-5 w-5 opacity-0 group-hover:opacity-100 transition-opacity text-destructive hover:text-destructive"
          onClick={onDelete}
        >
          <Trash2 className="h-3 w-3" />
        </Button>
      </div>

      {isExpanded && (
        <div className="mt-1 ml-4 p-2 bg-muted rounded text-xs font-mono whitespace-pre-wrap max-h-32 overflow-y-auto">
          {typeof variable.value === "string"
            ? variable.value
            : JSON.stringify(variable.value, null, 2)}
        </div>
      )}
    </div>
  );
}

export default ContextViewer;
