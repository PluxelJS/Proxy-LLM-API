import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type Run } from "./api";
export function Backups({ run }: { run: Run }) {
  const [database, setDatabase] = useState(""),
    [name, setName] = useState(""),
    [target, setTarget] = useState("");
  const backups = useQuery({
    queryKey: ["backups"],
    queryFn: () =>
      api<{ name: string; database: string; created: string }[]>("/backups"),
  });
  return (
    <section>
      <h2>PostgreSQL 备份与恢复</h2>
      <p>备份保存在工作区 backups 目录。恢复只允许新数据库，不覆盖现有数据。</p>
      <div className="grid">
        <label>
          备份数据库
          <input
            value={database}
            onChange={(e) => setDatabase(e.target.value)}
          />
        </label>
        <label>
          备份名称（留空使用时间）
          <input value={name} onChange={(e) => setName(e.target.value)} />
        </label>
      </div>
      <button
        disabled={!database}
        onClick={() => run(() => api("/pg/backup", { database, name }))}
      >
        创建一致性备份
      </button>
      <hr />
      <label>
        恢复来源
        <select value={name} onChange={(e) => setName(e.target.value)}>
          <option value="">选择备份</option>
          {backups.data?.map((b) => (
            <option key={b.name} value={b.name}>
              {b.name} · {b.database}
            </option>
          ))}
        </select>
      </label>
      <label>
        新数据库名
        <input value={target} onChange={(e) => setTarget(e.target.value)} />
      </label>
      <button
        disabled={!name || !target}
        onClick={() => run(() => api("/pg/restore", { name, target }))}
      >
        恢复到新数据库
      </button>
    </section>
  );
}
