import { useEffect, useRef } from "react";
import { Terminal as XTerm } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";

interface TerminalProps {
  websocketUrl: string;
  onClose?: () => void;
}

export function Terminal({ websocketUrl, onClose }: TerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!containerRef.current) return;

    const term = new XTerm({ cursorBlink: true, fontSize: 14, theme: { background: "#111110", foreground: "#e8e4de" } });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(containerRef.current);
    fitAddon.fit();

    const ws = new WebSocket(websocketUrl);
    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      term.onData((data) => ws.send(data));
    };
    ws.onmessage = (event) => {
      const data = event.data;
      if (typeof data === "string") {
        term.write(data);
      } else {
        term.write(new Uint8Array(data as ArrayBuffer));
      }
    };
    ws.onclose = () => {
      term.write("\r\n[Connection closed]\r\n");
    };

    const onResize = () => fitAddon.fit();
    window.addEventListener("resize", onResize);

    return () => {
      window.removeEventListener("resize", onResize);
      ws.close();
      term.dispose();
    };
  }, [websocketUrl]);

  return (
    <div style={{ position: "relative" }}>
      {onClose && (
        <button onClick={onClose} style={{ position: "absolute", top: 4, right: 4, zIndex: 10, background: "var(--bg-tertiary)", color: "var(--text-primary)", border: "1px solid var(--border-primary)", borderRadius: 4, padding: "2px 8px", cursor: "pointer" }}>
          Close
        </button>
      )}
      <div ref={containerRef} style={{ height: 400, background: "#111110", borderRadius: 6 }} />
    </div>
  );
}
