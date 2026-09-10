import { QueryClient } from "@tanstack/react-query";
export type Service = {
  id: string;
  revision: number;
  enabled: boolean;
  image: string;
  args: string[];
  env: Record<string, string>;
  ports: { host: string; published: number; target: number }[];
  mounts: { kind: string; source: string; target: string; readOnly: boolean }[];
  memory: number;
  cpus: number;
  user: string;
  restart: string;
};
export type Status = {
  id: string;
  revision: number;
  enabled: boolean;
  container: { state: string; running: boolean } | null;
  appliedRevision: number;
  error?: string;
};
export type Job = {
  id: string;
  kind: string;
  resource: string;
  status: string;
  stage: string;
  error?: string;
  started: string;
};
export type Node = { id: string; name: string };
let token = "";
export async function api<T>(
  path: string,
  data?: unknown,
  method?: string,
): Promise<T> {
  const res = await fetch("/api" + path, {
    method: method ?? (data === undefined ? "GET" : "POST"),
    headers: {
      "X-Runtime-Token": token,
      ...(data !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    body: data === undefined ? undefined : JSON.stringify(data),
  });
  const result = await res.json();
  if (!res.ok)
    throw new Error(
      typeof result.error === "string"
        ? result.error
        : (result.error?.message ?? `HTTP ${res.status}`),
    );
  return result;
}
export const client = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
});
export const labels: Record<string, string> = {
  postgres: "PostgreSQL",
  dragonfly: "DragonflyDB",
  vmetrics: "VictoriaMetrics",
  vlogs: "VictoriaLogs",
  "new-api": "New API",
  cliproxy: "CLIProxyAPI",
  "sing-box": "sing-box",
  cloudflared: "Cloudflared",
  "api-entry": "API 入口",
};

export type Run = (fn: () => Promise<unknown>) => Promise<any>;
export function setToken(value: string) {
  token = value;
}
