import { useAppData } from "../../contexts/AppContext";
import { DataTable, type Column } from "../../components/DataTable";

export function EnvironmentHierarchyPage() {
  const { dashboard } = useAppData();

  const columns: Column[] = [
    { key: "name", label: "Name" },
    { key: "slug", label: "Slug" },
    {
      key: "projectId",
      label: "Project",
      render: (v) => {
        const p = dashboard.projects.find((p) => p.id === v);
        return String(p?.name ?? v ?? "-");
      },
    },
    { key: "createdAt", label: "Created", render: (v) => (v ? new Date(String(v)).toLocaleDateString() : "") },
  ];

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <h2 style={{ margin: 0 }}>Environments</h2>
      <DataTable columns={columns} rows={dashboard.environments} loading={dashboard.loading} emptyMessage="No environments yet." />
    </div>
  );
}
