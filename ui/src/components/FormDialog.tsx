import type { ReactNode, FormEvent } from "react";

interface FormDialogProps {
  open: boolean;
  title: string;
  onClose: () => void;
  onSubmit: () => void;
  loading?: boolean;
  children: ReactNode;
}

export function FormDialog({ open, title, onClose, onSubmit, loading, children }: FormDialogProps) {
  if (!open) return null;

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit();
  };

  return (
    <div style={{ position: "fixed", inset: 0, background: "var(--bg-overlay)", zIndex: 1000, display: "flex", alignItems: "center", justifyContent: "center", animation: "fadeIn 120ms ease" }} onClick={onClose}>
      <form
        onSubmit={handleSubmit}
        onClick={(e) => e.stopPropagation()}
        style={{ background: "var(--bg-secondary)", borderRadius: "var(--radius-md)", padding: 24, minWidth: 400, maxWidth: 560, border: "1px solid var(--border-primary)", boxShadow: "var(--shadow-lg)", animation: "slideUp 180ms ease" }}
      >
        <h3 style={{ margin: "0 0 16px" }}>{title}</h3>
        <div style={{ display: "grid", gap: 12 }}>{children}</div>
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginTop: 20 }}>
          <button type="button" onClick={onClose} style={{ padding: "6px 16px" }}>
            Cancel
          </button>
          <button type="submit" disabled={loading} style={{ padding: "6px 16px", background: "var(--gold-500)", color: "var(--text-on-gold)", border: "none", borderRadius: 4, cursor: loading ? "wait" : "pointer", fontWeight: 600 }}>
            {loading ? "Saving..." : "Save"}
          </button>
        </div>
      </form>
    </div>
  );
}
