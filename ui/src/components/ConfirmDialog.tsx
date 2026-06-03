interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  loading?: boolean;
}

export function ConfirmDialog({ open, title, message, destructive, onConfirm, onCancel, loading }: ConfirmDialogProps) {
  if (!open) return null;

  return (
    <div style={{ position: "fixed", inset: 0, background: "var(--bg-overlay)", zIndex: 1000, display: "flex", alignItems: "center", justifyContent: "center", animation: "fadeIn 120ms ease" }} onClick={onCancel}>
      <div onClick={(e) => e.stopPropagation()} style={{ background: "var(--bg-secondary)", borderRadius: "var(--radius-md)", padding: 24, minWidth: 360, maxWidth: 480, border: "1px solid var(--border-primary)", boxShadow: "var(--shadow-lg)", animation: "slideUp 180ms ease" }}>
        <h3 style={{ margin: "0 0 8px" }}>{title}</h3>
        <p style={{ color: "var(--text-secondary)", margin: "0 0 20px" }}>{message}</p>
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={onCancel} style={{ padding: "6px 16px" }}>
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={loading}
            style={{
              padding: "6px 16px",
              background: destructive ? "#991b1b" : "var(--gold-500)",
              color: destructive ? "#fecaca" : "var(--text-on-gold)",
              border: "none",
              borderRadius: 4,
              cursor: loading ? "wait" : "pointer",
              fontWeight: 600,
            }}
          >
            {loading ? "Processing..." : "Confirm"}
          </button>
        </div>
      </div>
    </div>
  );
}
