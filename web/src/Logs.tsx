import { useState } from "react";
import { Button } from "@heroui/react/button";
import { api, labels } from "./api";
export function Logs() {
  const [id, setId] = useState("postgres"),
    [text, setText] = useState(""),
    [error, setError] = useState("");
  return (
    <section>
      <h2>容器日志</h2>
      <div className="inline-controls">
        <select
          aria-label="日志服务"
          value={id}
          onChange={(e) => setId(e.target.value)}
        >
          {Object.entries(labels).map(([k, v]) => (
            <option key={k} value={k}>
              {v}
            </option>
          ))}
        </select>
        <Button
          variant="secondary"
          onPress={async () => {
            try {
              setError("");
              setText((await api<{ text: string }>("/logs/" + id)).text);
            } catch (e) {
              setError((e as Error).message);
            }
          }}
        >
          读取最近 200 行
        </Button>
      </div>
      {error && <p className="error">{error}</p>}
      <pre>{text || "尚未读取日志"}</pre>
    </section>
  );
}
