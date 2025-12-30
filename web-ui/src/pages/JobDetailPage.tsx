import { useParams, Link } from "react-router-dom";
import { useJobDetail } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDate, formatRelativeTime } from "@/lib/utils";
import {
  ArrowLeft,
  RefreshCw,
  CheckCircle2,
  Clock,
  PlayCircle,
  XCircle,
  FileJson,
  Terminal,
  Folder,
  Copy,
  Check,
} from "lucide-react";
import { useState } from "react";

const stateConfig: Record<string, { variant: "default" | "success" | "warning" | "info" | "destructive"; icon: React.ReactNode; label: string }> = {
  queued: { variant: "warning", icon: <Clock className="h-4 w-4" />, label: "Queued" },
  running: { variant: "info", icon: <PlayCircle className="h-4 w-4" />, label: "Running" },
  ok: { variant: "success", icon: <CheckCircle2 className="h-4 w-4" />, label: "Completed" },
  error: { variant: "destructive", icon: <XCircle className="h-4 w-4" />, label: "Error" },
  canceled: { variant: "default", icon: <XCircle className="h-4 w-4" />, label: "Canceled" },
};

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Button variant="ghost" size="icon" onClick={handleCopy} className="h-8 w-8">
      {copied ? <Check className="h-4 w-4 text-green-500" /> : <Copy className="h-4 w-4" />}
    </Button>
  );
}

// Format a key for display (snake_case -> Title Case)
function formatKey(key: string): string {
  return key
    .replace(/_/g, " ")
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

// Render a value based on its type
function ResultValue({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (value === null || value === undefined) {
    return <span className="text-muted-foreground italic">—</span>;
  }

  if (typeof value === "boolean") {
    return (
      <Badge variant={value ? "success" : "secondary"} className="text-xs">
        {value ? "Yes" : "No"}
      </Badge>
    );
  }

  if (typeof value === "number") {
    return <span className="font-mono text-blue-600 dark:text-blue-400">{value.toLocaleString()}</span>;
  }

  if (typeof value === "string") {
    // Check if it's a long string or multiline
    if (value.length > 100 || value.includes("\n")) {
      return (
        <pre className="text-sm bg-muted/50 p-2 rounded mt-1 whitespace-pre-wrap break-words max-h-48 overflow-auto">
          {value}
        </pre>
      );
    }
    // Check if it looks like a path
    if (value.startsWith("/") || value.includes("\\")) {
      return <code className="text-xs bg-muted px-1.5 py-0.5 rounded font-mono">{value}</code>;
    }
    return <span>{value}</span>;
  }

  if (Array.isArray(value)) {
    if (value.length === 0) {
      return <span className="text-muted-foreground italic">Empty list</span>;
    }
    // For simple arrays (strings, numbers), show inline
    if (value.every((v) => typeof v === "string" || typeof v === "number")) {
      return (
        <div className="flex flex-wrap gap-1 mt-1">
          {value.map((item, i) => (
            <Badge key={i} variant="outline" className="text-xs font-normal">
              {String(item)}
            </Badge>
          ))}
        </div>
      );
    }
    // For complex arrays, show nested
    return (
      <div className="space-y-2 mt-1">
        {value.map((item, i) => (
          <div key={i} className="pl-3 border-l-2 border-muted">
            <span className="text-xs text-muted-foreground">#{i + 1}</span>
            <ResultValue value={item} depth={depth + 1} />
          </div>
        ))}
      </div>
    );
  }

  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);
    if (entries.length === 0) {
      return <span className="text-muted-foreground italic">Empty</span>;
    }
    return (
      <div className={`space-y-2 ${depth > 0 ? "mt-1 pl-3 border-l-2 border-muted" : ""}`}>
        {entries.map(([key, val]) => (
          <div key={key}>
            <span className="text-sm font-medium text-muted-foreground">{formatKey(key)}</span>
            <div className="mt-0.5">
              <ResultValue value={val} depth={depth + 1} />
            </div>
          </div>
        ))}
      </div>
    );
  }

  return <span>{String(value)}</span>;
}

// Main result display component
function ResultDisplay({ data }: { data: unknown }) {
  const [showRaw, setShowRaw] = useState(false);

  // Parse if string
  let parsed = data;
  if (typeof data === "string") {
    try {
      parsed = JSON.parse(data);
    } catch {
      // Not JSON, show as-is
      return (
        <pre className="text-sm p-4 bg-muted rounded-md whitespace-pre-wrap font-mono">
          {data}
        </pre>
      );
    }
  }

  const rawJson = typeof data === "string" ? data : JSON.stringify(data, null, 2);

  return (
    <div>
      <div className="flex justify-end mb-2">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setShowRaw(!showRaw)}
          className="text-xs"
        >
          {showRaw ? "Formatted" : "Raw JSON"}
        </Button>
      </div>
      {showRaw ? (
        <pre className="text-sm p-4 bg-muted rounded-md overflow-auto max-h-[500px] font-mono">
          {rawJson}
        </pre>
      ) : (
        <div className="p-4 bg-muted/30 rounded-md">
          <ResultValue value={parsed} />
        </div>
      )}
    </div>
  );
}

export function JobDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { data: job, isLoading, refetch, isFetching } = useJobDetail(id || "");

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!job) {
    return (
      <div className="text-center py-16">
        <p className="text-muted-foreground">Job not found</p>
        <Link to="/jobs" className="mt-4 inline-block">
          <Button variant="outline">
            <ArrowLeft className="h-4 w-4 mr-2" />
            Back to Jobs
          </Button>
        </Link>
      </div>
    );
  }

  const config = stateConfig[job.state] || stateConfig.queued;
  const hasResult = job.result_data !== null && job.result_data !== undefined;
  const resultJson = hasResult
    ? typeof job.result_data === "string"
      ? job.result_data
      : JSON.stringify(job.result_data, null, 2)
    : null;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Link to="/jobs">
            <Button variant="outline" size="icon">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
          <div className="flex-1">
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-bold">{job.skill || job.command}</h1>
              <Badge variant={config.variant} className="gap-1">
                {config.icon}
                {config.label}
              </Badge>
            </div>
            <p className="text-sm text-muted-foreground font-mono mt-1">{job.id}</p>
          </div>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={isFetching}
        >
          <RefreshCw className={`h-4 w-4 mr-2 ${isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      {/* Info cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground">Type</p>
            <p className="text-xl font-semibold mt-1">{job.type || "Unknown"}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground">Category</p>
            <p className="text-xl font-semibold mt-1">{job.category || "—"}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground">Skill</p>
            <p className="text-xl font-semibold mt-1">{job.skill || "—"}</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-muted-foreground">Created</p>
            <p className="text-lg font-semibold mt-1">{formatRelativeTime(job.created_at)}</p>
            <p className="text-xs text-muted-foreground">{formatDate(job.created_at)}</p>
          </CardContent>
        </Card>
      </div>

      {/* Error */}
      {job.error && (
        <Card className="border-destructive">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-destructive">
              <XCircle className="h-5 w-5" />
              Error
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="bg-destructive/10 p-4 rounded-md">
              <pre className="text-sm text-destructive whitespace-pre-wrap font-mono">
                {job.error}
              </pre>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Command */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2">
              <Terminal className="h-5 w-5" />
              Command
            </CardTitle>
            <CopyButton text={job.command} />
          </div>
        </CardHeader>
        <CardContent>
          <code className="text-sm bg-muted p-4 rounded-md block font-mono">
            {job.command}
          </code>
        </CardContent>
      </Card>

      {/* Artifacts */}
      {job.artifacts && job.artifacts.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Folder className="h-5 w-5" />
              Artifacts ({job.artifacts.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid gap-2 md:grid-cols-2">
              {job.artifacts.map((artifact) => (
                <div
                  key={artifact}
                  className="flex items-center gap-2 p-3 bg-muted rounded-md"
                >
                  <FileJson className="h-4 w-4 text-muted-foreground" />
                  <span className="text-sm font-mono truncate">{artifact}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Result */}
      {hasResult && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2">
                <FileJson className="h-5 w-5" />
                Result
              </CardTitle>
              {resultJson && <CopyButton text={resultJson} />}
            </div>
          </CardHeader>
          <CardContent>
            <ResultDisplay data={job.result_data} />
          </CardContent>
        </Card>
      )}

      {/* Stderr */}
      {job.stderr && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle className="flex items-center gap-2">
                <Terminal className="h-5 w-5" />
                Stderr
              </CardTitle>
              <CopyButton text={job.stderr} />
            </div>
          </CardHeader>
          <CardContent>
            <div className="bg-zinc-900 text-zinc-100 rounded-md overflow-hidden">
              <pre className="text-sm p-4 overflow-auto max-h-[400px] font-mono">
                {job.stderr}
              </pre>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
