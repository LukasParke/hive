import { useState } from "react";
import { useParams } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { LogViewer } from "../../components/LogViewer";

export function LogViewerPage() {
  const { containerID = "" } = useParams();
  const { session } = useAuth();
  const [tail, setTail] = useState(200);

  if (!session) return <p>Not authenticated.</p>;

  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const wsUrl = `${protocol}//${window.location.host}/api/v1/ws/logs/${containerID}?access_token=${encodeURIComponent(session.accessToken)}&follow=true&tail=${tail}`;

  return (
    <div style={{ display: "grid", gap: 12 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <h2 style={{ margin: 0 }}>Logs</h2>
        <select value={tail} onChange={(e) => setTail(Number(e.target.value))} style={{ padding: "4px 8px" }}>
          <option value={100}>100 lines</option>
          <option value={200}>200 lines</option>
          <option value={500}>500 lines</option>
          <option value={1000}>1000 lines</option>
        </select>
        <span style={{ color: "var(--text-secondary)", fontSize: 13 }}>{containerID}</span>
      </div>
      <LogViewer websocketUrl={wsUrl} tail={tail} />
    </div>
  );
}
