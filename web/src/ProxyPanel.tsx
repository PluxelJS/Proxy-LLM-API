import { useState } from "react";
import { Button } from "@heroui/react/button";
import { Input } from "@heroui/react/input";
import { Link } from "@heroui/react/link";
import { TextArea } from "@heroui/react/textarea";
import { ExternalLink } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { api, type Node, type Service, type Run } from "./api";

export function NodePanel({ run }: { run: Run }) {
  const nodes = useQuery({
    queryKey: ["nodes"],
    queryFn: () => api<{ nodes: Node[]; active: string }>("/nodes"),
  });
  const form = useForm<{ name: string; url: string }>();
  return (
    <section id="nodes" className="management-section">
      <div className="section-title">
        <div>
          <h2>节点管理</h2>
          <p className="muted">
            当前选择：
            {nodes.data?.active
              ? (nodes.data.nodes.find((node) => node.id === nodes.data.active)
                  ?.name ?? "未知节点")
              : "直连"}
          </p>
        </div>
        <Button
          variant="secondary"
          onPress={() => run(() => api("/nodes/direct/apply", {}))}
        >
          应用直连
        </Button>
      </div>
      {nodes.data?.nodes.map((node) => (
        <div className="node" key={node.id}>
          <strong>
            {node.name}
            {nodes.data.active === node.id ? " · 已选" : ""}
          </strong>
          <div className="actions compact-actions">
            <Button
              size="sm"
              variant="secondary"
              onPress={() => run(() => api("/nodes/" + node.id + "/apply", {}))}
            >
              应用
            </Button>
            <Button
              size="sm"
              variant="danger-soft"
              isDisabled={nodes.data.active === node.id}
              onPress={() => run(() => api("/nodes/" + node.id, {}, "DELETE"))}
            >
              删除
            </Button>
          </div>
        </div>
      ))}
      <form
        onSubmit={form.handleSubmit(async (value) => {
          const result = await run(() => api("/nodes", value));
          if (result) form.reset();
        })}
      >
        <label>
          名称
          <Input
            variant="secondary"
            {...form.register("name", { required: true })}
          />
        </label>
        <label>
          分享链接
          <TextArea
            variant="secondary"
            rows={3}
            {...form.register("url", { required: true })}
            placeholder="vless://…"
          />
        </label>
        <Button type="submit" variant="primary">
          保存节点
        </Button>
      </form>
    </section>
  );
}

export function CLIProxyPanel({ run }: { run: Run }) {
  const services = useQuery({
    queryKey: ["services"],
    queryFn: () => api<Service[]>("/services"),
  });
  const [result, setResult] = useState("");
  const proxy = services.data?.find((service) => service.id === "cliproxy");
  const port = proxy?.ports[0];
  return (
    <>
      <section id="diagnostics" className="management-section">
        <h2>出站诊断</h2>
        <p>使用当前全局代理请求测试目标，分别确认配置和请求结果。</p>
        <Button
          variant="primary"
          onPress={() =>
            run(async () => {
              const value = await api("/diagnose/cliproxy", {});
              setResult(JSON.stringify(value, null, 2));
              return value;
            })
          }
        >
          运行实际出站测试
        </Button>
        <pre>{result || "尚未测试"}</pre>
        <p className="muted">账号代理覆盖、授权及模型推理需要另行验证。</p>
      </section>
      <section id="accounts" className="management-section">
        <h2>账号</h2>
        <div className="actions">
          {port && (
            <Link
              href={`http://${port.host}:${port.published}/management.html`}
              target="_blank"
              rel="noreferrer"
            >
              打开管理页 <ExternalLink size={14} />
            </Link>
          )}
          <Button
            variant="secondary"
            onPress={() =>
              run(async () => {
                const value = await api<{ key: string }>("/management-key", {});
                await navigator.clipboard.writeText(value.key);
                return { message: "管理密钥已复制" };
              })
            }
          >
            复制管理密钥
          </Button>
        </div>
      </section>
    </>
  );
}
