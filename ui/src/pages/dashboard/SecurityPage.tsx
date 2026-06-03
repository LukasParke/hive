import { useState } from "react";
import { api, type SecurityRule } from "../../api/client";
import { useAuth } from "../../contexts/AuthContext";
import { useAppData } from "../../contexts/AppContext";
import { useToast } from "../../contexts/ToastContext";
import { DataTable, type Column } from "../../components/DataTable";
import { FormDialog } from "../../components/FormDialog";
import { ConfirmDialog } from "../../components/ConfirmDialog";

const RULE_TYPES = [
  { value: "ip_allowlist", label: "IP Allowlist" },
  { value: "ip_blocklist", label: "IP Blocklist" },
  { value: "header_security", label: "Header Security" },
  { value: "rate_limit", label: "Rate Limit" },
  { value: "country_block", label: "Country Block" },
];

export function SecurityPage() {
  const { session } = useAuth();
  const { dashboard, refreshSecurityRules } = useAppData();
  const toast = useToast();

  const [showCreate, setShowCreate] = useState(false);
  const [editing, setEditing] = useState<SecurityRule | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SecurityRule | null>(null);
  const [loading, setLoading] = useState(false);

  const [name, setName] = useState("");
  const [ruleType, setRuleType] = useState("ip_allowlist");
  const [applicationId, setApplicationId] = useState("");
  const [configJson, setConfigJson] = useState("{}");
  const [enabled, setEnabled] = useState(true);

  const resetForm = () => {
    setName("");
    setRuleType("ip_allowlist");
    setApplicationId(String(dashboard.applications[0]?.id ?? ""));
    setConfigJson("{}");
    setEnabled(true);
    setEditing(null);
  };

  const openCreate = () => {
    resetForm();
    setApplicationId(String(dashboard.applications[0]?.id ?? ""));
    setShowCreate(true);
  };

  const openEdit = (rule: SecurityRule) => {
    setEditing(rule);
    setName(rule.name);
    setRuleType(rule.type);
    setApplicationId(rule.applicationId ?? "");
    setConfigJson(JSON.stringify(rule.config ?? {}, null, 2));
    setEnabled(rule.enabled ?? true);
    setShowCreate(true);
  };

  const columns: Column[] = [
    { key: "name", label: "Name" },
    {
      key: "type",
      label: "Type",
      render: (v) => RULE_TYPES.find((t) => t.value === v)?.label ?? String(v),
    },
    {
      key: "applicationId",
      label: "Application",
      render: (v) => {
        const a = dashboard.applications.find((app) => app.id === v);
        return String(a?.name ?? v ?? "Global");
      },
    },
    {
      key: "enabled",
      label: "Enabled",
      render: (v) => (v ? "Yes" : "No"),
    },
    {
      key: "createdAt",
      label: "Created",
      render: (v) => (v ? new Date(String(v)).toLocaleDateString() : ""),
    },
    {
      key: "id",
      label: "",
      render: (_v, row) => (
        <div style={{ display: "flex", gap: 8 }}>
          <button
            onClick={(e) => {
              e.stopPropagation();
              openEdit(row as SecurityRule);
            }}
            style={{ fontSize: 12, background: "transparent", border: "none", color: "var(--gold-500)", cursor: "pointer" }}
          >
            Edit
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation();
              setDeleteTarget(row as SecurityRule);
            }}
            style={{ fontSize: 12, background: "transparent", border: "none", color: "var(--danger-text)", cursor: "pointer" }}
          >
            Delete
          </button>
        </div>
      ),
    },
  ];

  function parseConfig(): Record<string, unknown> {
    try {
      return JSON.parse(configJson);
    } catch {
      return {};
    }
  }

  async function handleSubmit() {
    if (!session || !name) return;
    const body = {
      name,
      type: ruleType as SecurityRule["type"],
      applicationId: applicationId || undefined,
      config: parseConfig() as unknown as Record<string, never>,
      enabled,
    };
    setLoading(true);
    try {
      if (editing) {
        await api.updateSecurityRule(session, String(editing.id), body);
        toast.success("Security rule updated");
      } else {
        await api.createSecurityRule(session, body);
        toast.success("Security rule created");
      }
      setShowCreate(false);
      resetForm();
      await refreshSecurityRules();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  async function handleDelete() {
    if (!session || !deleteTarget) return;
    setLoading(true);
    try {
      await api.deleteSecurityRule(session, String(deleteTarget.id));
      toast.success("Security rule deleted");
      setDeleteTarget(null);
      await refreshSecurityRules();
    } catch (err) {
      toast.error((err as Error).message);
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>Security Rules</h2>
        <button
          onClick={openCreate}
          style={{
            padding: "6px 16px",
            background: "var(--gold-500)",
            color: "var(--text-on-gold)",
            border: "none",
            borderRadius: 4,
            fontWeight: 600,
            cursor: "pointer",
          }}
        >
          Create Rule
        </button>
      </div>

      <DataTable
        columns={columns}
        rows={dashboard.securityRules}
        emptyMessage="No security rules yet."
      />

      <FormDialog
        open={showCreate}
        title={editing ? "Edit Security Rule" : "Create Security Rule"}
        onClose={() => setShowCreate(false)}
        onSubmit={handleSubmit}
        loading={loading}
      >
        <label>
          Name
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Office IP Allowlist"
            style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}
          />
        </label>
        <label>
          Type
          <select
            value={ruleType}
            onChange={(e) => setRuleType(e.target.value)}
            style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}
          >
            {RULE_TYPES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
        </label>
        <label>
          Application
          <select
            value={applicationId}
            onChange={(e) => setApplicationId(e.target.value)}
            style={{ width: "100%", padding: "6px 10px", marginTop: 4 }}
          >
            <option value="">Global (all apps)</option>
            {dashboard.applications.map((a) => (
              <option key={String(a.id)} value={String(a.id)}>
                {String(a.name)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Config (JSON)
          <textarea
            value={configJson}
            onChange={(e) => setConfigJson(e.target.value)}
            rows={5}
            placeholder='{"ips": ["192.168.1.0/24"]}'
            style={{ width: "100%", padding: "6px 10px", marginTop: 4, fontFamily: "monospace", fontSize: 13 }}
          />
        </label>
        <label style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 4 }}>
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          Enabled
        </label>
      </FormDialog>

      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete Security Rule"
        message={`Delete "${String(deleteTarget?.name ?? "")}"?`}
        destructive
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
        loading={loading}
      />
    </div>
  );
}
