import type { ReactNode } from "react";

import { theme } from "../theme";

export interface WorklistSection<T> {
  id: string;
  title: string;
  items: T[];
}

export function GroupedWorklist<T extends { id: string }>({
  sections,
  selectedId,
  renderItem,
  emptyState,
}: {
  sections: WorklistSection<T>[];
  selectedId: string | null;
  renderItem: (item: T, selected: boolean) => ReactNode;
  emptyState: ReactNode;
}) {
  const itemCount = sections.reduce(
    (total, section) => total + section.items.length,
    0,
  );
  if (itemCount === 0) {
    return <>{emptyState}</>;
  }

  return (
    <scrollbox style={{ flexGrow: 1, marginTop: 1 }}>
      {sections.map((section) => {
        if (section.items.length === 0) return null;
        return (
          <box
            key={section.id}
            style={{ flexDirection: "column", marginBottom: 1 }}
          >
            <text fg={theme.muted}>
              {section.title} ({section.items.length})
            </text>
            {section.items.map((item) =>
              renderItem(item, item.id === selectedId),
            )}
          </box>
        );
      })}
    </scrollbox>
  );
}
