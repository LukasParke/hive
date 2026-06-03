import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

interface Toast {
  id: number;
  message: string;
  type: "success" | "error";
}

interface ToastContextValue {
  success: (msg: string) => void;
  error: (msg: string) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

let nextId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const push = useCallback((message: string, type: "success" | "error") => {
    const id = ++nextId;
    setToasts((prev) => [...prev, { id, message, type }]);
    setTimeout(() => setToasts((prev) => prev.filter((t) => t.id !== id)), 4000);
  }, []);

  const success = useCallback((msg: string) => push(msg, "success"), [push]);
  const error = useCallback((msg: string) => push(msg, "error"), [push]);

  const value = useMemo(() => ({ success, error }), [success, error]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      {toasts.length > 0 && (
        <div style={{ position: "fixed", top: 16, right: 16, zIndex: 9999, display: "grid", gap: 8 }}>
          {toasts.map((t) => (
            <div
              key={t.id}
              style={{
                padding: "10px 16px",
                borderRadius: "var(--radius-md)",
                background: "var(--bg-secondary)",
                color: "var(--text-primary)",
                fontSize: 14,
                boxShadow: "var(--shadow-lg)",
                maxWidth: 360,
                border: "1px solid var(--border-primary)",
                borderLeft: t.type === "success" ? "4px solid #4ade80" : "4px solid #f87171",
                animation: "toastIn 180ms ease",
              }}
            >
              {t.message}
            </div>
          ))}
        </div>
      )}
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within ToastProvider");
  return ctx;
}
