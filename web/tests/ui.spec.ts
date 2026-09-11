import { test, expect } from "@playwright/test";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { spawn, execFileSync, type ChildProcess } from "node:child_process";
import { createServer } from "node:net";

const state = mkdtempSync(`${tmpdir()}/dev-runtime-ui-`);
const binary = resolve("../bin/dev-runtime");
let daemon: ChildProcess;
let origin: string;

test.beforeAll(async () => {
  const listener = createServer();
  await new Promise<void>((r) => listener.listen(0, "127.0.0.1", r));
  const port = (listener.address() as { port: number }).port;
  await new Promise<void>((r) => listener.close(() => r()));
  origin = `http://127.0.0.1:${port}`;
  // Deliberately unavailable engine: browser tests never touch real containers.
  execFileSync(binary, [
    "--state-dir",
    state,
    "init",
    "--engine",
    "podman",
    "--endpoint",
    "unix:///tmp/dev-runtime-ui-no-engine.sock",
  ]);
  daemon = spawn(
    binary,
    ["--state-dir", state, "serve", "--listen", `127.0.0.1:${port}`],
    { stdio: "ignore" },
  );
  await expect
    .poll(async () => {
      try {
        return (await fetch(origin)).status;
      } catch {
        return 0;
      }
    })
    .toBe(200);
});
test.afterAll(async () => {
  if (daemon?.exitCode === null) {
    daemon.kill("SIGTERM");
    await new Promise<void>((r) => daemon.once("exit", () => r()));
  }
  rmSync(state, { recursive: true, force: true });
});

test("dashboard navigation, CLI-shared configuration and node validation", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(origin);
  await expect(
    page.getByRole("heading", { name: "Dev Runtime" }),
  ).toBeVisible();
  await expect(page.getByRole("navigation", { name: "主导航" })).toBeVisible();
  await expect(page.getByRole("grid", { name: "服务概览" })).toBeVisible();
  await page.getByLabel("筛选服务").fill("proxy");
  await expect(
    page.getByRole("button", { name: "CLIProxyAPI", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "PostgreSQL", exact: true }),
  ).toHaveCount(0);
  await page.getByLabel("筛选服务").clear();
  for (const [name, heading] of [
    ["总览", "运行总览"],
    ["任务与日志", "任务与日志"],
    ["设置", "工作区设置"],
  ]) {
    await page.getByRole("button", { name, exact: true }).click();
    await expect(
      page.getByRole("heading", { name: heading, exact: true }),
    ).toBeVisible();
  }
  await page.getByRole("button", { name: "PostgreSQL", exact: true }).click();
  await expect(
    page.getByRole("navigation", { name: "PostgreSQL 页面目录" }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "数据库" })).toHaveAttribute(
    "aria-current",
    "location",
  );
  await page.locator("#backups").evaluate((element) => {
    document.documentElement.style.scrollBehavior = "auto";
    const toolbarHeight =
      document.querySelector<HTMLElement>(".service-toolbar")?.offsetHeight ??
      88;
    window.scrollTo(
      0,
      window.scrollY + element.getBoundingClientRect().top - toolbarHeight + 1,
    );
  });
  await expect(page.getByRole("link", { name: "备份" })).toHaveAttribute(
    "aria-current",
    "location",
  );
  await expect(page.getByRole("heading", { name: "备份与恢复" })).toBeVisible();
  await page.getByRole("button", { name: "DragonflyDB", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "DragonflyDB：重启" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "编辑配置" }).click();
  await page.getByLabel("maxmemory", { exact: true }).fill("2gb");
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("保存");
  const exported = JSON.parse(
    execFileSync(
      binary,
      ["--state-dir", state, "config", "export", "--reveal"],
      { encoding: "utf8" },
    ),
  );
  expect(
    exported.services.find((s: any) => s.id === "dragonfly").args,
  ).toContain("--maxmemory=2gb");
  await page.getByRole("button", { name: "sing-box", exact: true }).click();
  await page.getByLabel("名称", { exact: true }).fill("test-node");
  await page
    .getByLabel("分享链接", { exact: true })
    .fill("socks5://127.0.0.1:1081");
  await page.getByRole("button", { name: "保存节点" }).click();
  await expect(page.getByText("test-node", { exact: true })).toBeVisible();
  const nodes = JSON.parse(
    execFileSync(binary, ["--state-dir", state, "nodes", "list"], {
      encoding: "utf8",
    }),
  );
  expect(nodes.data[0].name).toBe("test-node");
  expect(nodes.data[0].url).toBeUndefined();
  await page.getByRole("button", { name: "总览", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "服务", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("grid", { name: "服务概览" })).toBeVisible();
  expect(errors).toEqual([]);
});
