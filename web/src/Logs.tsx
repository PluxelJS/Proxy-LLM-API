import { useState } from "react";
import { Alert } from "@heroui/react/alert";
import { Button } from "@heroui/react/button";
import { Toolbar } from "@heroui/react/toolbar";
import { api, labels } from "./api";
import { SelectField } from "./Fields";
export function Logs() {
  const [id, setId] = useState("postgres"),
    [text, setText] = useState(""),
    [error, setError] = useState("");
  return (
    <section>
      <h2>容器日志</h2>
      <Toolbar className="log-controls" aria-label="日志查询">
        <SelectField
          label="服务"
          value={id}
          options={Object.entries(labels).map(([key, label]) => ({
            id: key,
            label,
          }))}
          onChange={setId}
        />
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
      </Toolbar>
      {error && (
        <Alert status="danger">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Description>{error}</Alert.Description>
          </Alert.Content>
        </Alert>
      )}
      <pre>{text || "尚未读取日志"}</pre>
    </section>
  );
}
