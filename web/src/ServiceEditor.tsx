import { useState } from "react";
import { Button } from "@heroui/react/button";
import { Input } from "@heroui/react/input";
import { TextArea } from "@heroui/react/textarea";
import { useQuery } from "@tanstack/react-query";
import { api, labels, type Service, type Run } from "./api";
export function ServiceEditor({
  id,
  busy,
  run,
}: {
  id: string;
  busy: boolean;
  run: Run;
}) {
  const config = useQuery({
    queryKey: ["config", id],
    queryFn: async () =>
      (await api<Service[]>("/services?reveal=true")).find((s) => s.id === id)!,
  });
  const [draft, setDraft] = useState<Service | null>(null),
    [args, setArgs] = useState(""),
    [env, setEnv] = useState(""),
    [mounts, setMounts] = useState(""),
    [diagnosis, setDiagnosis] = useState(""),
    [plan, setPlan] = useState("");
  const s = draft ?? config.data;
  function edit() {
    if (config.data) {
      setDraft(structuredClone(config.data));
      setArgs(JSON.stringify(config.data.args, null, 2));
      setEnv(JSON.stringify(config.data.env, null, 2));
      setMounts(JSON.stringify(config.data.mounts, null, 2));
    }
  }
  const patch = (key: keyof Service, value: unknown) =>
    setDraft((v) => ({ ...v!, [key]: value }));
  function dragonValue(name: string) {
    try {
      return (
        (JSON.parse(args) as string[])
          .find((a) => a.startsWith("--" + name + "="))
          ?.split("=")
          .slice(1)
          .join("=") ?? ""
      );
    } catch {
      return "";
    }
  }
  function dragonSet(name: string, value: string) {
    try {
      const argv = JSON.parse(args) as string[];
      const prefix = "--" + name + "=";
      const idx = argv.findIndex((a) => a.startsWith(prefix));
      if (idx < 0) argv.push(prefix + value);
      else argv[idx] = prefix + value;
      setArgs(JSON.stringify(argv, null, 2));
    } catch {}
  }
  if (!s)
    return (
      <section id="config">{config.error?.message ?? "读取配置…"}</section>
    );
  return (
    <section id="config" className="management-section">
      <div className="section-title">
        <div>
          <h2>配置</h2>
          <small>
            {labels[id]} · 配置版本 {s.revision}
            {draft && config.data?.revision !== draft.revision
              ? " · 服务已被其他操作修改，保存会检查冲突"
              : ""}
          </small>
        </div>
        <Button variant="secondary" onPress={edit}>
          {draft ? "放弃编辑并重新载入" : "编辑配置"}
        </Button>
      </div>
      <div className="actions">
        <Button
          variant="secondary"
          isDisabled={busy}
          onPress={() =>
            run(() =>
              api(`/services/${id}/pull`, { revision: config.data?.revision }),
            )
          }
        >
          拉取镜像
        </Button>
        <Button
          variant="secondary"
          onPress={() =>
            run(async () => {
              const v = await api(`/diagnose/${id}`, {});
              setDiagnosis(JSON.stringify(v, null, 2));
              return v;
            })
          }
        >
          连通性检查
        </Button>
      </div>
      {diagnosis && <pre>{diagnosis}</pre>}
      {draft ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            run(async () => {
              const next = {
                ...draft,
                args: JSON.parse(args),
                env: JSON.parse(env),
                mounts: JSON.parse(mounts),
              };
              const v = await api(`/services/${id}`, next, "PUT");
              setDraft(null);
              return v;
            });
          }}
        >
          <label>
            镜像
            <Input
              variant="secondary"
              value={draft.image}
              onChange={(e) => patch("image", e.target.value)}
            />
          </label>
          <div className="grid">
            <label>
              容器内存上限（字节，0 不限制）
              <Input
                variant="secondary"
                type="number"
                min="0"
                value={draft.memory}
                onChange={(e) => patch("memory", Number(e.target.value))}
              />
            </label>
            <label>
              CPU 上限（0 不限制）
              <Input
                variant="secondary"
                type="number"
                min="0"
                step="0.1"
                value={draft.cpus}
                onChange={(e) => patch("cpus", Number(e.target.value))}
              />
            </label>
          </div>
          {draft.ports.map((p, i) => (
            <div className="grid" key={i}>
              <label>
                宿主监听地址
                <Input
                  variant="secondary"
                  value={p.host}
                  onChange={(e) =>
                    patch(
                      "ports",
                      draft.ports.map((v, j) =>
                        i === j ? { ...v, host: e.target.value } : v,
                      ),
                    )
                  }
                />
              </label>
              <label>
                宿主端口
                <Input
                  variant="secondary"
                  type="number"
                  value={p.published}
                  onChange={(e) =>
                    patch(
                      "ports",
                      draft.ports.map((v, j) =>
                        i === j
                          ? { ...v, published: Number(e.target.value) }
                          : v,
                      ),
                    )
                  }
                />
              </label>
              <label>
                容器端口
                <Input
                  variant="secondary"
                  type="number"
                  value={p.target}
                  onChange={(e) =>
                    patch(
                      "ports",
                      draft.ports.map((v, j) =>
                        i === j ? { ...v, target: Number(e.target.value) } : v,
                      ),
                    )
                  }
                />
              </label>
            </div>
          ))}
          <div className="grid">
            <label>
              容器用户
              <Input
                variant="secondary"
                value={draft.user}
                onChange={(e) => patch("user", e.target.value)}
                placeholder="镜像默认用户"
              />
            </label>
            <label>
              重启策略
              <select
                value={draft.restart}
                onChange={(e) => patch("restart", e.target.value)}
              >
                <option>unless-stopped</option>
                <option>on-failure</option>
                <option>no</option>
              </select>
            </label>
          </div>
          {id === "dragonfly" && (
            <fieldset>
              <legend>Dragonfly 常用参数</legend>
              <div className="grid">
                {[
                  "maxmemory",
                  "proactor_threads",
                  "snapshot_cron",
                  "dbfilename",
                  "default_lua_flags",
                ].map((name) => (
                  <label key={name}>
                    {name}
                    <Input
                      variant="secondary"
                      value={dragonValue(name)}
                      onChange={(e) => dragonSet(name, e.target.value)}
                    />
                  </label>
                ))}
              </div>
            </fieldset>
          )}
          <label>
            完整启动参数 argv（JSON 数组）
            <TextArea
              variant="secondary"
              rows={9}
              value={args}
              onChange={(e) => setArgs(e.target.value)}
            />
          </label>
          <p className="muted">
            每个数组元素是一个参数；不会经过 shell
            拆分。保留数据路径和配置文件参数，或同步修改挂载。
          </p>
          <details>
            <summary>环境变量（可能包含凭据）</summary>
            <TextArea
              variant="secondary"
              rows={9}
              value={env}
              onChange={(e) => setEnv(e.target.value)}
            />
          </details>
          <details>
            <summary>数据挂载（变更后可能连接到不同的数据）</summary>
            <TextArea
              variant="secondary"
              rows={9}
              value={mounts}
              onChange={(e) => setMounts(e.target.value)}
            />
          </details>
          <Button type="submit" variant="primary" isDisabled={busy}>
            保存配置
          </Button>
        </form>
      ) : (
        <>
          <p className="mono">{s.image}</p>
          <pre>{JSON.stringify(s.args, null, 2)}</pre>
        </>
      )}
      <div className="actions">
        <Button
          variant="secondary"
          onPress={() =>
            run(async () => {
              const v = await api(`/services/${id}/plan`);
              setPlan(JSON.stringify(v, null, 2));
              return { message: "变更计划已生成" };
            })
          }
        >
          查看应用计划
        </Button>
        <Button
          variant="primary"
          isDisabled={busy || !!draft}
          onPress={() =>
            run(() =>
              api(`/services/${id}/apply`, { revision: config.data?.revision }),
            )
          }
        >
          应用已保存配置
        </Button>
      </div>
      {plan && <pre>{plan}</pre>}
    </section>
  );
}
