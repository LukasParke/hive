import { useEffect, useRef, useState } from "react";

interface LogViewerProps {
  websocketUrl: string;
  follow?: boolean;
  tail?: number;
}

export function LogViewer({ websocketUrl, follow = true, tail }: LogViewerProps) {
  const [lines, setLines] = useState<string[]>([]);
  const [isFollowing, setIsFollowing] = useState(follow);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const ws = new WebSocket(websocketUrl);
    ws.onmessage = (event) => {
      setLines((prev) => {
        const next = [...prev, String(event.data)];
        return tail ? next.slice(-tail) : next.slice(-5000);
      });
    };
    ws.onclose = () => {
      setLines((prev) => [...prev, "[Connection closed]"]);
    };
    return () => ws.close();
  }, [websocketUrl, tail]);

  useEffect(() => {
    if (isFollowing && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [lines, isFollowing]);

  return (
    <div>
      <div style={{ display: "flex", gap: 8, marginBottom: 8 }}>
        <label style={{ fontSize: 13, color: "var(--text-secondary)" }}>
          <input type="checkbox" checked={isFollowing} onChange={(e) => setIsFollowing(e.target.checked)} /> Follow
        </label>
      </div>
      <div
        ref={containerRef}
        style={{
          height: 400,
          overflowY: "auto",
          background: "#111110",
          color: "#e8e4de",
          fontFamily: "var(--font-mono)",
          fontSize: 13,
          padding: 12,
          borderRadius: 6,
          whiteSpace: "pre-wrap",
          wordBreak: "break-all",
        }}
      >
        {lines.map((line, i) => (
          <div key={i}>{line}</div>
        ))}
      </div>
    </div>
  );
}
