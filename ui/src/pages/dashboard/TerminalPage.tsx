import { useState } from "react";
import { useParams } from "react-router-dom";
import { useAuth } from "../../contexts/AuthContext";
import { Terminal } from "../../components/Terminal";

export function TerminalPage() {
  const { containerID = "" } = useParams();
  const { session } = useAuth();
  const [shell, setShell] = useState("/bin/sh");

  if (!session) return <p>Not authenticated.</p>;

  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
  const wsUrl = `${protocol}//${window.location.host}/api/v1/ws/terminal/${containerID}?access_token=${encodeURIComponent(session.accessToken)}&shell=${encodeURIComponent(shell)}`;

  return (
    <div style={{ display: "grid", gap: 12 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <h2 style={{ margin: 0 }}>Terminal</h2>
        <select value={shell} onChange={(e) => setShell(e.target.value)} style={{ padding: "4px 8px" }}>
          <option value="/bin/sh">/bin/sh</option>
          <option value="/bin/bash">/bin/bash</option>
          <option value="/bin/zsh">/bin/zsh</option>
        </select>
        <span style={{ color: "var(--text-secondary)", fontSize: 13 }}>{containerID}</span>
      </div>
      <Terminal websocketUrl={wsUrl} />
    </div>
  );
}
