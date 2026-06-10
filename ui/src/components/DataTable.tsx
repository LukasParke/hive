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
  loading?: boolean;
}

export function DataTable({ columns, rows, onRowClick, emptyMessage = "No items yet.", loading = false }: DataTableProps) {
  if (loading) {
    return (
      <div className="card" style={{ padding: 28 }}>
        <div className="empty-state">Loading…</div>
      </div>
    );
  }

  if (rows.length === 0) {
    return (
      <div className="card" style={{ padding: 40 }}>
        <div className="empty-state">{emptyMessage}</div>
      </div>
    );
  }

  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            {columns.map((col) => (
              <th key={col.key}>{col.label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr
              key={String(row.id ?? i)}
              onClick={() => onRowClick?.(row)}
              onKeyDown={(event) => {
                if (!onRowClick) return;
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  onRowClick(row);
                }
              }}
              tabIndex={onRowClick ? 0 : undefined}
              role={onRowClick ? "button" : undefined}
              style={{ cursor: onRowClick ? "pointer" : "default" }}
            >
              {columns.map((col) => (
                <td key={col.key}>
                  {col.render ? col.render(row[col.key], row) : String(row[col.key] ?? "—")}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
