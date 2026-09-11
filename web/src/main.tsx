import { useState } from "react";
import { createRoot } from "react-dom/client";
import { Button } from "@heroui/react/button";
import { Chip } from "@heroui/react/chip";
import { Input } from "@heroui/react/input";
import {
  QueryClientProvider,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  Activity,
  ChevronRight,
  ListChecks,
  Play,
  Power,
  PowerOff,
  RefreshCw,
  RotateCcw,
  Settings,
  Square,
} from "lucide-react";
import {
  api,
  client,
  labels,
  setToken,
  type Job,
  type Run,
  type Status,
} from "./api";
import "./style.css";
import { Backups } from "./Backups";
import { JobRow } from "./JobRow";
import { Logs } from "./Logs";
import { PGPanel } from "./PGPanel";
import { CLIProxyPanel, NodePanel } from "./ProxyPanel";
import { ServiceEditor } from "./ServiceEditor";

type View = "overview" | "jobs" | "settings" | string;

function serviceState(service?: Status) {
  if (!service) return "读取状态中";
  if (service.error) return "引擎不可用";
  return service.container?.running
    ? "运行中"
    : (service.container?.state ?? "尚未创建");
}

function ServiceNav({
  id,
  service,
  selected,
  busy,
  onSelect,
  run,
}: {
  id: string;
  service?: Status;
  selected: boolean;
  busy: boolean;
  onSelect: (id: string) => void;
  run: Run;
}) {
  const label = labels[id];
  const action = (name: string) =>
    run(() => api(`/services/${id}/${name}`, { revision: service?.revision }));
  const unavailable = busy || !service || !!service.error;
  const running = !!service?.container?.running;
  const lifecycle = [
    ["start", "启动", Play],
    ["stop", "停止", Square],
    ["restart", "重启", RotateCcw],
  ] as const;

  return (
    <div className={"service-nav " + (selected ? "selected" : "")}>
      <Button
        size="sm"
        variant={selected ? "secondary" : "ghost"}
        className="service-link"
        aria-label={label}
        aria-current={selected ? "page" : undefined}
        onPress={() => onSelect(id)}
      >
        <span
          className={"dot " + (service?.container?.running ? "green" : "")}
        />
        <span className="service-link-text">
          <span>{label}</span>
          <small>{serviceState(service)}</small>
        </span>
        {service && service.revision !== service.appliedRevision && (
          <span
            className="pending-mark"
            title="配置待应用"
            aria-label="配置待应用"
          >
            !
          </span>
        )}
      </Button>
      <div className="service-actions" aria-label={`${label} 操作`}>
        {lifecycle.map(([name, title, Icon]) => (
          <span className="icon-tooltip" title={title} key={name}>
            <Button
              size="sm"
              variant="ghost"
              isIconOnly
              className="icon-button"
              isDisabled={
                unavailable || (name === "start" ? running : !running)
              }
              aria-label={`${label}：${title}`}
              onPress={() => action(name)}
            >
              <Icon
                size={14}
                fill={name === "start" ? "currentColor" : "none"}
              />
            </Button>
          </span>
        ))}
        <span
          className="icon-tooltip"
          title={service?.enabled ? "关闭自启动并停止" : "启用自启动并启动"}
        >
          <Button
            size="sm"
            variant="ghost"
            isIconOnly
            className={
              "icon-button auto-start " + (service?.enabled ? "enabled" : "")
            }
            isDisabled={unavailable}
            aria-label={`${label}：${service?.enabled ? "关闭自启动并停止" : "启用自启动并启动"}`}
            onPress={() => action(service?.enabled ? "disable" : "enable")}
          >
            {service?.enabled ? <Power size={14} /> : <PowerOff size={14} />}
          </Button>
        </span>
      </div>
    </div>
  );
}

function ServiceToolbar({
  service,
  busy,
  run,
  sections,
}: {
  service?: Status;
  busy: boolean;
  run: Run;
  sections: [string, string][];
}) {
  if (!service)
    return <div className="service-toolbar muted">读取服务状态…</div>;
  const running = !!service.container?.running;
  const unavailable = busy || !!service.error;
  const action = (name: string) =>
    run(() =>
      api(`/services/${service.id}/${name}`, { revision: service.revision }),
    );
  return (
    <div className="service-toolbar">
      <div
        className="service-toolbar-main"
        aria-label={`${labels[service.id]} 当前状态与操作`}
      >
        <div className="service-summary">
          <strong>{labels[service.id]}</strong>
          <Chip
            size="sm"
            color={running ? "success" : "default"}
            variant="soft"
          >
            {serviceState(service)}
          </Chip>
          <span>v{service.revision}</span>
          {service.revision !== service.appliedRevision && (
            <span className="pending-text">
              当前运行 v{service.appliedRevision}
            </span>
          )}
        </div>
        <div className="toolbar-actions">
          <Button
            size="sm"
            variant="secondary"
            isDisabled={unavailable || running}
            onPress={() => action("start")}
          >
            <Play size={14} fill="currentColor" /> 启动
          </Button>
          <Button
            size="sm"
            variant="secondary"
            isDisabled={unavailable || !running}
            onPress={() => action("stop")}
          >
            <Square size={14} /> 停止
          </Button>
          <Button
            size="sm"
            variant="secondary"
            isDisabled={unavailable || !running}
            onPress={() => action("restart")}
          >
            <RotateCcw size={14} /> 重启
          </Button>
          <Button
            size="sm"
            variant={service.enabled ? "danger-soft" : "secondary"}
            isDisabled={unavailable}
            onPress={() => action(service.enabled ? "disable" : "enable")}
          >
            {service.enabled ? <Power size={14} /> : <PowerOff size={14} />}
            {service.enabled ? "关闭自启动并停止" : "启用自启动并启动"}
          </Button>
        </div>
      </div>
      <nav className="page-toc" aria-label={`${labels[service.id]} 页面目录`}>
        {sections.map(([target, title]) => (
          <a key={target} href={`#${target}`}>
            {title}
          </a>
        ))}
      </nav>
    </div>
  );
}

function serviceSections(id: string): [string, string][] {
  if (id === "postgres")
    return [
      ["databases", "数据库"],
      ["users", "用户"],
      ["backups", "备份"],
      ["config", "配置"],
    ];
  if (id === "sing-box")
    return [
      ["nodes", "节点"],
      ["config", "配置"],
    ];
  if (id === "cliproxy")
    return [
      ["diagnostics", "诊断"],
      ["accounts", "账号"],
      ["config", "配置"],
    ];
  return [["config", "配置"]];
}

function Overview({
  rows,
  jobs,
  onSelect,
}: {
  rows: Status[];
  jobs: Job[];
  onSelect: (id: string) => void;
}) {
  const running = rows.filter((service) => service.container?.running).length;
  const pending = rows.filter(
    (service) => service.revision !== service.appliedRevision,
  ).length;
  const activeJobs = jobs.filter((job) =>
    ["running", "queued"].includes(job.status),
  );

  return (
    <>
      <div className="stats" aria-label="运行摘要">
        <article>
          <small>正在运行</small>
          <strong>
            {running}
            <i> / {rows.length}</i>
          </strong>
        </article>
        <article>
          <small>待应用配置</small>
          <strong>{pending}</strong>
        </article>
        <article>
          <small>执行中任务</small>
          <strong>{activeJobs.length}</strong>
        </article>
      </div>
      <section className="page-section">
        <div className="section-title">
          <div>
            <h2>服务</h2>
            <p className="muted">选择服务进入对应的管理页面。</p>
          </div>
        </div>
        <div className="service-grid">
          {Object.keys(labels).map((id) => {
            const service = rows.find((item) => item.id === id);
            return (
              <Button
                variant="secondary"
                className="service-card"
                key={id}
                onPress={() => onSelect(id)}
              >
                <span className="service-card-heading">
                  <span
                    className={
                      "dot " + (service?.container?.running ? "green" : "")
                    }
                  />
                  <strong>{labels[id]}</strong>
                  <ChevronRight size={16} />
                </span>
                <span className="service-card-state">
                  {serviceState(service)}
                </span>
                <span className="service-card-meta">
                  {service?.enabled ? "自启动已启用" : "未启用自启动"}
                  {service && service.revision !== service.appliedRevision
                    ? ` · 待应用 v${service.revision}`
                    : ""}
                </span>
              </Button>
            );
          })}
        </div>
      </section>
      {jobs[0] && (
        <section className="page-section">
          <h2>最近操作</h2>
          <JobRow job={jobs[0]} />
        </section>
      )}
    </>
  );
}

function ServicePage({
  id,
  busy,
  run,
}: {
  id: string;
  busy: boolean;
  run: Run;
}) {
  if (id === "postgres") {
    return (
      <>
        <PGPanel run={run} />
        <Backups run={run} />
        <ServiceEditor id={id} busy={busy} run={run} />
      </>
    );
  }
  if (id === "sing-box") {
    return (
      <>
        <NodePanel run={run} />
        <ServiceEditor id={id} busy={busy} run={run} />
      </>
    );
  }
  if (id === "cliproxy") {
    return (
      <>
        <CLIProxyPanel run={run} />
        <ServiceEditor id={id} busy={busy} run={run} />
      </>
    );
  }
  return <ServiceEditor id={id} busy={busy} run={run} />;
}

function SettingsPage({ run }: { run: Run }) {
  return (
    <>
      <section className="page-section">
        <h2>配置导出</h2>
        <p>
          每个服务使用带 revision 的配置文档。CLI 与网页修改同一份 SQLite
          管理数据。
        </p>
        <Button
          variant="secondary"
          onPress={() =>
            run(async () => {
              const data = await api("/config");
              const blob = new Blob([JSON.stringify(data, null, 2)], {
                type: "application/json",
              });
              const url = URL.createObjectURL(blob);
              const link = document.createElement("a");
              link.href = url;
              link.download = "dev-runtime-private-config.json";
              link.click();
              URL.revokeObjectURL(url);
              return { message: "私有配置已导出，包含环境变量中的凭据" };
            })
          }
        >
          导出完整私有配置
        </Button>
        <p className="muted">
          配置文件包含凭据，请保存在私有位置。业务数据库和数据卷需独立备份。
        </p>
      </section>
      <section className="page-section">
        <h2>导入服务配置</h2>
        <input
          type="file"
          accept="application/json,.json"
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file)
              run(async () =>
                api("/config", JSON.parse(await file.text()), "PUT"),
              );
          }}
        />
        <p className="muted">保存配置，不自动重启服务。版本冲突会拒绝导入。</p>
      </section>
      <section className="page-section">
        <h2>Agent CLI</h2>
        <pre>{`dev-runtime status --json\ndev-runtime services inspect dragonfly --reveal\ndev-runtime config save service.json\ndev-runtime config apply dragonfly\ndev-runtime pg database create demo --generate-password\ndev-runtime completion zsh --code`}</pre>
      </section>
    </>
  );
}

function App() {
  const [view, setView] = useState<View>("overview");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);
  const [serviceFilter, setServiceFilter] = useState("");
  const q = useQueryClient();
  const status = useQuery({
    queryKey: ["status"],
    queryFn: () => api<Status[]>("/status"),
    refetchInterval: 5000,
  });
  const jobs = useQuery({
    queryKey: ["jobs"],
    queryFn: () => api<Job[]>("/jobs"),
    refetchInterval: 1500,
  });
  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    setError(false);
    try {
      const result: any = await fn();
      setNotice(
        result?.status === "queued"
          ? `已提交任务 ${result.id.slice(0, 8)}，可在任务与日志查看进度`
          : (result?.message ?? "操作完成"),
      );
      q.invalidateQueries();
      return result;
    } catch (e) {
      setError(true);
      setNotice((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const rows = status.data ?? [];
  const sorted = [...(jobs.data ?? [])].sort((a, b) =>
    b.started.localeCompare(a.started),
  );
  const page =
    view === "overview"
      ? ["运行总览", "查看所有服务和当前任务。"]
      : view === "jobs"
        ? ["任务与日志", "查看后台操作进度和容器日志。"]
        : ["工作区设置", "导入、导出共享的本地服务配置。"];
  const visibleServices = Object.keys(labels).filter((id) =>
    labels[id].toLocaleLowerCase().includes(serviceFilter.toLocaleLowerCase()),
  );

  return (
    <main className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="eyebrow">LOCAL WORKSPACE</span>
          <h1>Dev Runtime</h1>
          <p>本地服务工作区</p>
        </div>
        <nav className="side-nav" aria-label="主导航">
          <span className="nav-heading">工作台</span>
          <Button
            size="sm"
            variant={view === "overview" ? "secondary" : "ghost"}
            className={"nav-link " + (view === "overview" ? "selected" : "")}
            onPress={() => setView("overview")}
          >
            <Activity size={16} /> 总览
          </Button>
          <Button
            size="sm"
            variant={view === "jobs" ? "secondary" : "ghost"}
            className={"nav-link " + (view === "jobs" ? "selected" : "")}
            onPress={() => setView("jobs")}
          >
            <ListChecks size={16} /> 任务与日志
          </Button>
          <Button
            size="sm"
            variant={view === "settings" ? "secondary" : "ghost"}
            className={"nav-link " + (view === "settings" ? "selected" : "")}
            onPress={() => setView("settings")}
          >
            <Settings size={16} /> 设置
          </Button>
          <span className="nav-heading services-heading">服务</span>
          <Input
            className="service-search"
            variant="secondary"
            aria-label="筛选服务"
            placeholder="筛选服务"
            value={serviceFilter}
            onChange={(event) => setServiceFilter(event.target.value)}
          />
          {visibleServices.map((id) => (
            <ServiceNav
              key={id}
              id={id}
              service={rows.find((service) => service.id === id)}
              selected={view === id}
              busy={busy}
              onSelect={setView}
              run={run}
            />
          ))}
        </nav>
        <div className="sidebar-summary">
          <span className="dot green" />{" "}
          {rows.filter((service) => service.container?.running).length} /{" "}
          {rows.length || "-"} 运行中
        </div>
      </aside>
      <div className="workspace">
        {!(view in labels) && (
          <header className="workspace-header">
            <div>
              <span className="eyebrow">DASHBOARD</span>
              <h2>{page[0]}</h2>
              <p>{page[1]}</p>
            </div>
            <span className="icon-tooltip" title="刷新数据">
              <Button
                size="sm"
                variant="ghost"
                isIconOnly
                className="icon-button refresh"
                aria-label="刷新数据"
                onPress={() => q.invalidateQueries()}
              >
                <RefreshCw size={16} />
              </Button>
            </span>
          </header>
        )}
        {notice && (
          <div role="status" className={"notice " + (error ? "error" : "")}>
            {notice}
          </div>
        )}
        {status.error && (
          <div className="notice error">{status.error.message}</div>
        )}
        {view in labels && (
          <ServiceToolbar
            service={rows.find((service) => service.id === view)}
            busy={busy}
            run={run}
            sections={serviceSections(view)}
          />
        )}
        <div className="page-content">
          {view === "overview" && (
            <Overview rows={rows} jobs={sorted} onSelect={setView} />
          )}
          {view === "jobs" && (
            <>
              <section className="page-section">
                <h2>操作记录</h2>
                {sorted.length ? (
                  sorted.map((job) => <JobRow key={job.id} job={job} />)
                ) : (
                  <p className="muted">还没有操作记录。</p>
                )}
              </section>
              <Logs />
            </>
          )}
          {view === "settings" && <SettingsPage run={run} />}
          {view in labels && (
            <ServicePage key={view} id={view} busy={busy} run={run} />
          )}
        </div>
        <footer>Linux / WSL · Docker / Podman · 本地私有工作区</footer>
      </div>
    </main>
  );
}

api<{ token: string }>("/session")
  .then((session) => {
    setToken(session.token);
    createRoot(document.getElementById("root")!).render(
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>,
    );
  })
  .catch((e) => {
    document.getElementById("root")!.textContent =
      "无法连接管理服务：" + e.message;
  });
