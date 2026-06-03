import type { ItemMap } from "../api/client";

export interface Column {
  key: string;
  label: string;
  render?: (value: unknown, row: ItemMap) => React.ReactNode;
}

interface DataTableProps {
  columns: Column[];
  rows: ItemMap[];
  onRowClick?: (row: ItemMap) => void;
  emptyMessage?: string;
}

export function DataTable({ columns, rows, onRowClick, emptyMessage = "No items yet." }: DataTableProps) {
  if (rows.length === 0) {
    return <p style={{ color: "var(--text-secondary)", padding: 12 }}>{emptyMessage}</p>;
  }

  return (
    <div style={{ overflowX: "auto" }}>
      <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 14 }}>
        <thead>
          <tr>
            {columns.map((col) => (
              <th key={col.key} style={{ textAlign: "left", padding: "8px 12px", borderBottom: "2px solid var(--border-secondary)", fontWeight: 600, whiteSpace: "nowrap", color: "var(--text-faint)", fontSize: 11, textTransform: "uppercase", letterSpacing: "0.5px" }}>
                {col.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr
              key={String(row.id ?? i)}
              onClick={() => onRowClick?.(row)}
              style={{ cursor: onRowClick ? "pointer" : "default", borderBottom: "1px solid var(--border-primary)", transition: "background var(--transition-fast)" }}
              onMouseEnter={(e) => { if (onRowClick) e.currentTarget.style.background = "var(--bg-tertiary)"; }}
              onMouseLeave={(e) => { e.currentTarget.style.background = ""; }}
            >
              {columns.map((col) => (
                <td key={col.key} style={{ padding: "8px 12px" }}>
                  {col.render ? col.render(row[col.key], row) : String(row[col.key] ?? "")}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
