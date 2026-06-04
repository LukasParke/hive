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
    <div className="dialog-overlay" onClick={onCancel}>
      <div onClick={(e) => e.stopPropagation()} className="dialog-panel" style={{ maxWidth: 420 }}>
        <div className="dialog-header">
          <h3>{title}</h3>
          <p>{message}</p>
        </div>
        <div className="dialog-footer">
          <button onClick={onCancel} className="btn-ghost">
            Cancel
          </button>
          <button
            onClick={onConfirm}
            disabled={loading}
            className={destructive ? "btn-danger" : "btn-primary"}
          >
            {loading ? "Processing…" : "Confirm"}
          </button>
        </div>
      </div>
    </div>
  );
}
