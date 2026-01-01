import { useState } from "react";
import { useWorkspaces } from "@/api/hooks";
import { switchWorkspace } from "@/api/client";
import { Select } from "@/components/ui/select";
import { FolderOpen, AlertCircle } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";

interface WorkspaceFilterProps {
  onWorkspaceChange?: (workspace: string) => void;
}

export function WorkspaceFilter({ onWorkspaceChange }: WorkspaceFilterProps) {
  const { data: workspacesData } = useWorkspaces();
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const handleWorkspaceChange = async (
    e: React.ChangeEvent<HTMLSelectElement>
  ) => {
    const workspace = e.target.value;
    setError(null);
    setIsLoading(true);

    try {
      await switchWorkspace(workspace);
      // Invalidate all queries to refetch with new workspace
      queryClient.invalidateQueries();
      onWorkspaceChange?.(workspace);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to switch workspace";
      setError(message);
      // Don't invalidate queries if switch failed
    } finally {
      setIsLoading(false);
    }
  };

  if (!workspacesData?.workspaces || workspacesData.workspaces.length === 0) {
    return null;
  }

  return (
    <div className="flex items-center gap-2">
      <FolderOpen className="h-4 w-4 text-muted-foreground" />
      <Select
        value={workspacesData.current || ""}
        onChange={handleWorkspaceChange}
        className="w-56"
        disabled={isLoading}
      >
        <option value="">All workspaces</option>
        {workspacesData.workspaces.map((ws) => (
          <option key={ws.path} value={ws.path} title={ws.path}>
            {ws.name}
          </option>
        ))}
      </Select>
      {workspacesData.current && (
        <span className="text-xs text-muted-foreground truncate max-w-48" title={workspacesData.current}>
          {workspacesData.current}
        </span>
      )}
      {error && (
        <span className="flex items-center gap-1 text-xs text-destructive" title={error}>
          <AlertCircle className="h-3 w-3" />
          Error
        </span>
      )}
    </div>
  );
}
