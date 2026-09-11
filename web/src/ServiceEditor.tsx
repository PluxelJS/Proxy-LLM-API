import { useState } from "react";
import { Button } from "@heroui/react/button";
import { Disclosure } from "@heroui/react/disclosure";
import { Fieldset } from "@heroui/react/fieldset";
import { Form } from "@heroui/react/form";
import { Surface } from "@heroui/react/surface";
import { Toolbar } from "@heroui/react/toolbar";
import { useQuery } from "@tanstack/react-query";
import { api, labels, type Service, type Run } from "./api";
import {
  SelectField,
  TextAreaField,
  TextInputField,
  type SelectOption,
} from "./Fields";

const restartPolicies: SelectOption[] = [
  { id: "unless-stopped", label: "unless-stopped" },
  { id: "on-failure", label: "on-failure" },
  { id: "no", label: "no" },
];

const dragonflyArgs = [
  "maxmemory",
  "proactor_threads",
  "snapshot_cron",
  "dbfilename",
  "default_lua_flags",
];

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
      (await api<Service[]>("/services?reveal=true")).find(
        (service) => service.id === id,
      )!,
  });
  const [draft, setDraft] = useState<Service | null>(null);
  const [args, setArgs] = useState("");
  const [env, setEnv] = useState("");
  const [mounts, setMounts] = useState("");
  const [diagnosis, setDiagnosis] = useState("");
  const [plan, setPlan] = useState("");
  const service = draft ?? config.data;

  function edit() {
    if (!config.data) return;
    setDraft(structuredClone(config.data));
    setArgs(JSON.stringify(config.data.args, null, 2));
    setEnv(JSON.stringify(config.data.env, null, 2));
    setMounts(JSON.stringify(config.data.mounts, null, 2));
  }

  const patch = (key: keyof Service, value: unknown) =>
    setDraft((valueBefore) => ({ ...valueBefore!, [key]: value }));

  function dragonValue(name: string) {
    try {
      return (
        (JSON.parse(args) as string[])
          .find((arg) => arg.startsWith("--" + name + "="))
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
      const index = argv.findIndex((arg) => arg.startsWith(prefix));
      if (index < 0) argv.push(prefix + value);
      else argv[index] = prefix + value;
      setArgs(JSON.stringify(argv, null, 2));
    } catch {}
  }

  if (!service)
    return (
      <section id="config">{config.error?.message ?? "读取配置…"}</section>
    );

  return (
    <section id="config" className="management-section">
      <header className="section-title">
        <div>
          <h2>配置</h2>
          <p className="muted">
            {labels[id]} · 版本 {service.revision}
            {draft && config.data?.revision !== draft.revision
              ? " · 服务已被其他操作修改，保存会检查冲突"
              : ""}
          </p>
        </div>
        <Button variant="secondary" onPress={edit}>
          {draft ? "放弃编辑并重新载入" : "编辑配置"}
        </Button>
      </header>

      <Toolbar className="section-toolbar" aria-label="配置工具">
        <Button
          variant="secondary"
          isDisabled={busy}
          onPress={() =>
            run(() =>
              api(`/services/${id}/pull`, {
                revision: config.data?.revision,
              }),
            )
          }
        >
          拉取镜像
        </Button>
        <Button
          variant="secondary"
          onPress={() =>
            run(async () => {
              const value = await api(`/diagnose/${id}`, {});
              setDiagnosis(JSON.stringify(value, null, 2));
              return value;
            })
          }
        >
          连通性检查
        </Button>
      </Toolbar>
      {diagnosis && <pre>{diagnosis}</pre>}

      {draft ? (
        <Surface className="form-surface" variant="secondary">
          <Form
            onSubmit={(event) => {
              event.preventDefault();
              run(async () => {
                const next = {
                  ...draft,
                  args: JSON.parse(args),
                  env: JSON.parse(env),
                  mounts: JSON.parse(mounts),
                };
                const value = await api(`/services/${id}`, next, "PUT");
                setDraft(null);
                return value;
              });
            }}
          >
            <Fieldset>
              <Fieldset.Legend>运行参数</Fieldset.Legend>
              <Fieldset.Group>
                <TextInputField
                  label="镜像"
                  value={draft.image}
                  onChange={(event) => patch("image", event.target.value)}
                />
                <div className="grid">
                  <TextInputField
                    label="容器内存上限（字节，0 不限制）"
                    type="number"
                    min="0"
                    value={draft.memory}
                    onChange={(event) =>
                      patch("memory", Number(event.target.value))
                    }
                  />
                  <TextInputField
                    label="CPU 上限（0 不限制）"
                    type="number"
                    min="0"
                    step="0.1"
                    value={draft.cpus}
                    onChange={(event) =>
                      patch("cpus", Number(event.target.value))
                    }
                  />
                </div>
                {draft.ports.map((port, index) => (
                  <div className="grid port-grid" key={index}>
                    <TextInputField
                      label="宿主监听地址"
                      value={port.host}
                      onChange={(event) =>
                        patch(
                          "ports",
                          draft.ports.map((value, portIndex) =>
                            index === portIndex
                              ? { ...value, host: event.target.value }
                              : value,
                          ),
                        )
                      }
                    />
                    <TextInputField
                      label="宿主端口"
                      type="number"
                      value={port.published}
                      onChange={(event) =>
                        patch(
                          "ports",
                          draft.ports.map((value, portIndex) =>
                            index === portIndex
                              ? {
                                  ...value,
                                  published: Number(event.target.value),
                                }
                              : value,
                          ),
                        )
                      }
                    />
                    <TextInputField
                      label="容器端口"
                      type="number"
                      value={port.target}
                      onChange={(event) =>
                        patch(
                          "ports",
                          draft.ports.map((value, portIndex) =>
                            index === portIndex
                              ? { ...value, target: Number(event.target.value) }
                              : value,
                          ),
                        )
                      }
                    />
                  </div>
                ))}
                <div className="grid">
                  <TextInputField
                    label="容器用户"
                    value={draft.user}
                    onChange={(event) => patch("user", event.target.value)}
                    placeholder="镜像默认用户"
                  />
                  <SelectField
                    label="重启策略"
                    value={draft.restart}
                    options={restartPolicies}
                    onChange={(value) => patch("restart", value)}
                  />
                </div>

                {id === "dragonfly" && (
                  <Fieldset>
                    <Fieldset.Legend>Dragonfly 常用参数</Fieldset.Legend>
                    <Fieldset.Group>
                      <div className="grid">
                        {dragonflyArgs.map((name) => (
                          <TextInputField
                            key={name}
                            label={name}
                            value={dragonValue(name)}
                            onChange={(event) =>
                              dragonSet(name, event.target.value)
                            }
                          />
                        ))}
                      </div>
                    </Fieldset.Group>
                  </Fieldset>
                )}

                <TextAreaField
                  label="完整启动参数 argv（JSON 数组）"
                  description="每个数组元素是一个参数，不经过 shell 拆分；保留数据路径和配置文件参数，或同步修改挂载。"
                  rows={9}
                  value={args}
                  onChange={(event) => setArgs(event.target.value)}
                />
                <Disclosure>
                  <Disclosure.Heading>
                    <Disclosure.Trigger>
                      环境变量（可能包含凭据）
                      <Disclosure.Indicator />
                    </Disclosure.Trigger>
                  </Disclosure.Heading>
                  <Disclosure.Content>
                    <Disclosure.Body>
                      <TextAreaField
                        label="环境变量 JSON"
                        rows={9}
                        value={env}
                        onChange={(event) => setEnv(event.target.value)}
                      />
                    </Disclosure.Body>
                  </Disclosure.Content>
                </Disclosure>
                <Disclosure>
                  <Disclosure.Heading>
                    <Disclosure.Trigger>
                      数据挂载（变更后可能连接到不同的数据）
                      <Disclosure.Indicator />
                    </Disclosure.Trigger>
                  </Disclosure.Heading>
                  <Disclosure.Content>
                    <Disclosure.Body>
                      <TextAreaField
                        label="挂载 JSON"
                        rows={9}
                        value={mounts}
                        onChange={(event) => setMounts(event.target.value)}
                      />
                    </Disclosure.Body>
                  </Disclosure.Content>
                </Disclosure>
              </Fieldset.Group>
              <Fieldset.Actions>
                <Button type="submit" variant="primary" isDisabled={busy}>
                  保存配置
                </Button>
              </Fieldset.Actions>
            </Fieldset>
          </Form>
        </Surface>
      ) : (
        <Surface className="config-preview" variant="secondary">
          <p className="mono">{service.image}</p>
          <pre>{JSON.stringify(service.args, null, 2)}</pre>
        </Surface>
      )}

      <Toolbar className="section-toolbar" aria-label="应用配置">
        <Button
          variant="secondary"
          onPress={() =>
            run(async () => {
              const value = await api(`/services/${id}/plan`);
              setPlan(JSON.stringify(value, null, 2));
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
              api(`/services/${id}/apply`, {
                revision: config.data?.revision,
              }),
            )
          }
        >
          应用已保存配置
        </Button>
      </Toolbar>
      {plan && <pre>{plan}</pre>}
    </section>
  );
}
