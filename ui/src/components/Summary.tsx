import type { ItemMap } from "../api/client";

export function Summary({ title, items }: { title: string; items: ItemMap[] }) {
  return (
    <section style={{ border: "1px solid var(--border-primary)", borderRadius: "var(--radius-md)", padding: 12, marginBottom: 12, background: "var(--bg-secondary)" }}>
      <h3>
        {title} ({items.length})
      </h3>
      {items.length === 0 ? (
        <p style={{ color: "var(--text-secondary)" }}>No items yet.</p>
      ) : (
        <pre style={{ background: "var(--code-bg)", padding: 10, overflowX: "auto", borderRadius: 6, color: "var(--text-primary)", border: "1px solid var(--border-primary)" }}>{JSON.stringify(items, null, 2)}</pre>
      )}
    </section>
  );
}
