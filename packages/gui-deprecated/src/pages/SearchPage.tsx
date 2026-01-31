import { useState } from "react";
import { useSearch } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Card, CardContent } from "@/components/ui/card";
import { truncate } from "@/lib/utils";
import { Search, RefreshCw, ChevronDown, ChevronUp, FileCode, MessageSquare, Brain, ListTodo, Copy, Check } from "lucide-react";
import type { SearchResult } from "@/types";

const sourceConfig: Record<string, { variant: "default" | "secondary" | "info" | "success" | "warning"; icon: React.ReactNode; label: string }> = {
  symbol: { variant: "default", icon: <FileCode className="h-4 w-4" />, label: "Code" },
  session: { variant: "info", icon: <MessageSquare className="h-4 w-4" />, label: "Session" },
  memory: { variant: "success", icon: <Brain className="h-4 w-4" />, label: "Memory" },
  task: { variant: "warning", icon: <ListTodo className="h-4 w-4" />, label: "Task" },
};

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Button variant="ghost" size="icon" onClick={handleCopy} className="h-7 w-7">
      {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
    </Button>
  );
}

function ResultCard({ result, index, isExpanded, onToggle }: {
  result: SearchResult;
  index: number;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const config = sourceConfig[result.source] || sourceConfig.symbol;

  return (
    <Card className={`transition-all ${isExpanded ? "ring-2 ring-primary" : ""}`}>
      <CardContent className="p-4">
        <div
          className="flex items-start justify-between cursor-pointer"
          onClick={onToggle}
        >
          <div className="flex items-start gap-3 flex-1">
            <div className="flex items-center justify-center w-8 h-8 rounded-full bg-muted text-sm font-medium">
              {index + 1}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-1">
                <Badge variant={config.variant} className="gap-1">
                  {config.icon}
                  {config.label}
                </Badge>
                <span className="text-xs text-muted-foreground">
                  Score: {result.final_score?.toFixed(4) || result.similarity?.toFixed(4)}
                </span>
              </div>
              <p className="font-medium truncate">
                {result.name || result.path || result.id}
              </p>
              {result.path && result.name && (
                <p className="text-sm text-muted-foreground truncate font-mono">
                  {result.path}
                </p>
              )}
            </div>
          </div>
          <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0">
            {isExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
          </Button>
        </div>

        {isExpanded && (
          <div className="mt-4 pt-4 border-t space-y-3">
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
              <div>
                <p className="text-muted-foreground">Source</p>
                <p className="font-medium">{result.source}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Similarity</p>
                <p className="font-mono">{result.similarity?.toFixed(4) || "—"}</p>
              </div>
              {result.rerank_score !== undefined && result.rerank_score > 0 && (
                <div>
                  <p className="text-muted-foreground">Rerank Score</p>
                  <p className="font-mono">{result.rerank_score.toFixed(4)}</p>
                </div>
              )}
              <div>
                <p className="text-muted-foreground">Final Score</p>
                <p className="font-mono font-semibold">{result.final_score?.toFixed(4) || "—"}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Global Rank</p>
                <p className="font-medium">#{result.rank}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Source Rank</p>
                <p className="font-medium">#{result.source_rank}</p>
              </div>
            </div>

            <div>
              <div className="flex items-center justify-between mb-1">
                <p className="text-sm text-muted-foreground">ID</p>
                <CopyButton text={result.id} />
              </div>
              <code className="text-xs bg-muted p-2 rounded block font-mono break-all">
                {result.id}
              </code>
            </div>

            {result.path && (
              <div>
                <div className="flex items-center justify-between mb-1">
                  <p className="text-sm text-muted-foreground">
                    Path{result.line ? `:${result.line}` : ""}
                  </p>
                  <CopyButton text={result.path} />
                </div>
                <code className="text-xs bg-muted p-2 rounded block font-mono break-all">
                  {result.path}{result.line ? `:${result.line}` : ""}
                </code>
              </div>
            )}

            {/* Content: Snippet for code, Summary for memories/sessions */}
            {result.snippet && (
              <div>
                <div className="flex items-center justify-between mb-1">
                  <p className="text-sm text-muted-foreground">Code Snippet</p>
                  <CopyButton text={result.snippet} />
                </div>
                <pre className="text-xs bg-zinc-900 text-zinc-100 p-3 rounded overflow-x-auto max-h-48 font-mono">
                  {result.snippet}
                </pre>
              </div>
            )}

            {result.summary && (
              <div>
                <div className="flex items-center justify-between mb-1">
                  <p className="text-sm text-muted-foreground">
                    {result.source === "memory" ? "Memory" : result.source === "session" ? "Session Summary" : "Summary"}
                  </p>
                  <CopyButton text={result.summary} />
                </div>
                <div className="text-sm bg-muted/50 p-3 rounded whitespace-pre-wrap">
                  {result.summary}
                </div>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function SearchPage() {
  const [query, setQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [rerank, setRerank] = useState(false);
  const [scope, setScope] = useState("");
  const [limit, setLimit] = useState(50);
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);

  const { data, isLoading, isFetching } = useSearch({
    q: submittedQuery,
    limit,
    rerank,
    scope: scope || undefined,
  });

  const results = data?.results || [];
  const stats = data?.stats;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setSubmittedQuery(query);
    setExpandedIndex(null);
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Semantic Search</h1>

      {/* Search form */}
      <Card>
        <CardContent className="pt-6">
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="flex gap-4">
              <div className="flex-1">
                <Input
                  placeholder="Search code, sessions, memories..."
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  className="h-11"
                />
              </div>
              <Button type="submit" disabled={!query || isFetching} className="h-11">
                <Search className="h-4 w-4 mr-2" />
                Search
              </Button>
            </div>
            <div className="flex gap-4 items-center flex-wrap">
              <div className="w-40">
                <Select value={scope} onChange={(e) => setScope(e.target.value)}>
                  <option value="">All sources</option>
                  <option value="symbols">Code (symbols)</option>
                  <option value="sessions">Sessions</option>
                  <option value="memories">Memories</option>
                  <option value="tasks">Tasks</option>
                </Select>
              </div>
              <div className="w-32">
                <Select value={String(limit)} onChange={(e) => setLimit(Number(e.target.value))}>
                  <option value="25">25 results</option>
                  <option value="50">50 results</option>
                  <option value="100">100 results</option>
                </Select>
              </div>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  checked={rerank}
                  onChange={(e) => setRerank(e.target.checked)}
                  className="rounded"
                />
                <span className="text-sm">Enable reranking</span>
              </label>
            </div>
          </form>
        </CardContent>
      </Card>

      {/* Stats */}
      {stats && submittedQuery && (
        <div className="flex gap-3 flex-wrap">
          <Badge variant="outline" className="text-sm py-1">
            {stats.total_results} results
          </Badge>
          <Badge variant="outline" className="text-sm py-1">
            {stats.latency_ms}ms
          </Badge>
          {stats.embedding_dimensions > 0 && (
            <Badge variant="outline" className="text-sm py-1">
              {stats.embedding_dimensions}d vectors
            </Badge>
          )}
          {stats.reranked && (
            <Badge variant="info" className="text-sm py-1">
              Reranked
            </Badge>
          )}
          {Object.entries(stats.source_counts || {}).map(([source, count]) => {
            const config = sourceConfig[source];
            return (
              <Badge key={source} variant={config?.variant || "secondary"} className="text-sm py-1 gap-1">
                {config?.icon}
                {source}: {count}
              </Badge>
            );
          })}
        </div>
      )}

      {/* Results */}
      {submittedQuery && (
        <div>
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold">
              Results for "{truncate(submittedQuery, 40)}"
            </h2>
            {results.length > 0 && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setExpandedIndex(expandedIndex === null ? 0 : null)}
              >
                {expandedIndex !== null ? "Collapse All" : "Expand First"}
              </Button>
            )}
          </div>

          {isLoading ? (
            <div className="flex items-center justify-center py-16">
              <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : results.length === 0 ? (
            <Card>
              <CardContent className="py-16 text-center text-muted-foreground">
                No results found for "{submittedQuery}"
              </CardContent>
            </Card>
          ) : (
            <div className="space-y-3">
              {results.map((result, i) => (
                <ResultCard
                  key={`${result.source}-${result.id}`}
                  result={result}
                  index={i}
                  isExpanded={expandedIndex === i}
                  onToggle={() => setExpandedIndex(expandedIndex === i ? null : i)}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
