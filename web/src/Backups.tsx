import { useState } from "react";
import { Button } from "@heroui/react/button";
import { Fieldset } from "@heroui/react/fieldset";
import { Surface } from "@heroui/react/surface";
import { useQuery } from "@tanstack/react-query";
import { api, type Run } from "./api";
import { SelectField, TextInputField } from "./Fields";
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
    <section id="backups" className="management-section">
      <h2>备份与恢复</h2>
      <p>备份保存在工作区 backups 目录。恢复只允许新数据库，不覆盖现有数据。</p>
      <Surface className="form-surface" variant="secondary">
        <Fieldset>
          <Fieldset.Legend>创建备份</Fieldset.Legend>
          <Fieldset.Group>
            <div className="grid">
              <TextInputField
                label="备份数据库"
                value={database}
                onChange={(event) => setDatabase(event.target.value)}
              />
              <TextInputField
                label="备份名称"
                description="留空时使用当前时间"
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </div>
          </Fieldset.Group>
          <Fieldset.Actions>
            <Button
              variant="primary"
              isDisabled={!database}
              onPress={() => run(() => api("/pg/backup", { database, name }))}
            >
              创建一致性备份
            </Button>
          </Fieldset.Actions>
        </Fieldset>
      </Surface>
      <Surface className="form-surface" variant="secondary">
        <Fieldset>
          <Fieldset.Legend>恢复备份</Fieldset.Legend>
          <Fieldset.Group>
            <SelectField
              label="恢复来源"
              value={name}
              options={(backups.data ?? []).map((backup) => ({
                id: backup.name,
                label: `${backup.name} · ${backup.database}`,
              }))}
              onChange={setName}
            />
            <TextInputField
              label="新数据库名"
              value={target}
              onChange={(event) => setTarget(event.target.value)}
            />
          </Fieldset.Group>
          <Fieldset.Actions>
            <Button
              variant="primary"
              isDisabled={!name || !target}
              onPress={() => run(() => api("/pg/restore", { name, target }))}
            >
              恢复到新数据库
            </Button>
          </Fieldset.Actions>
        </Fieldset>
      </Surface>
    </section>
  );
}
