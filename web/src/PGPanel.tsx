import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { api, type Run } from "./api";
export function PGPanel({ run }: { run: Run }) {
  const pg = useQuery({
    queryKey: ["pg"],
    queryFn: () =>
      api<{
        databases: { name: string; owner: string; managed: boolean }[];
        users: {
          name: string;
          login: boolean;
          superuser: boolean;
          passwordKnown: boolean;
        }[];
      }>("/pg"),
    retry: false,
  });
  const create = useForm<{
    database: string;
    user: string;
    password: string;
    generatePassword: boolean;
    existingUser: boolean;
  }>({
    defaultValues: {
      generatePassword: true,
      existingUser: false,
      password: "",
      user: "",
    },
  });
  const user = useForm<{
    user: string;
    password: string;
    generatePassword: boolean;
    database: string;
    permission: string;
    confirm: boolean;
  }>({
    defaultValues: {
      generatePassword: true,
      password: "",
      permission: "readonly",
      database: "",
      confirm: false,
    },
  });
  const [action, setAction] = useState("password"),
    [connection, setConnection] = useState(""),
    [confirmDB, setConfirmDB] = useState("");
  return (
    <>
      <section>
        <h2>数据库与用户</h2>
        {pg.error && (
          <p className="notice error">
            {pg.error.message}。请先在下方启动 PostgreSQL。
          </p>
        )}
        <table>
          <thead>
            <tr>
              <th>数据库</th>
              <th>Owner</th>
              <th>管理状态</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {pg.data?.databases.map((d) => (
              <tr key={d.name}>
                <td>{d.name}</td>
                <td>{d.owner}</td>
                <td>{d.managed ? "已登记" : "外部/系统"}</td>
                <td>
                  {d.managed && (
                    <button
                      onClick={() =>
                        run(async () => {
                          const v: any = await api("/pg/connection", {
                            database: d.name,
                            user: d.owner,
                          });
                          setConnection(v.url);
                          return { message: "连接串已显示，包含私有密码" };
                        })
                      }
                    >
                      显示连接串
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {connection && (
          <div>
            <pre>{connection}</pre>
            <button
              onClick={() =>
                run(async () => {
                  await navigator.clipboard.writeText(connection);
                  return { message: "连接串已复制" };
                })
              }
            >
              复制
            </button>
            <button onClick={() => setConnection("")}>隐藏</button>
          </div>
        )}
        <h3>新建数据库 + 登录用户</h3>
        <form
          onSubmit={create.handleSubmit((v) =>
            run(() => api("/pg/databases", v)),
          )}
        >
          <div className="grid">
            <label>
              数据库名
              <input
                {...create.register("database", { required: true })}
                placeholder="demo"
              />
            </label>
            <label>
              用户（留空生成数据库名_owner）
              <input {...create.register("user")} />
            </label>
          </div>
          <label className="check">
            <input type="checkbox" {...create.register("existingUser")} />
            选择已存在的用户
          </label>
          <label className="check">
            <input type="checkbox" {...create.register("generatePassword")} />
            自动生成密码
          </label>
          {!create.watch("generatePassword") && (
            <label>
              密码
              <input type="password" {...create.register("password")} />
            </label>
          )}
          <button className="primary">创建并验证</button>
        </form>
      </section>
      <section>
        <h2>登录角色</h2>
        <table>
          <thead>
            <tr>
              <th>用户名</th>
              <th>登录</th>
              <th>密码</th>
            </tr>
          </thead>
          <tbody>
            {pg.data?.users.map((u) => (
              <tr key={u.name}>
                <td>
                  {u.name}
                  {u.superuser ? " · 管理员" : ""}
                </td>
                <td>{u.login ? "允许" : "禁用"}</td>
                <td>{u.passwordKnown ? "工具已保存" : "未知 / 系统凭据"}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <form
          onSubmit={user.handleSubmit((v) =>
            run(() => api("/pg/users/" + action, v)),
          )}
        >
          <div className="grid">
            <label>
              用户名
              <input {...user.register("user", { required: true })} />
            </label>
            <label>
              操作
              <select
                value={action}
                onChange={(e) => setAction(e.target.value)}
              >
                {[
                  ["password", "重置密码"],
                  ["create", "创建用户"],
                  ["grant", "添加数据库权限"],
                  ["enable", "启用登录"],
                  ["disable", "禁用登录"],
                  ["drop", "删除用户"],
                ].map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
          </div>
          {["password", "create"].includes(action) && (
            <>
              <label className="check">
                <input type="checkbox" {...user.register("generatePassword")} />
                自动生成密码
              </label>
              {!user.watch("generatePassword") && (
                <label>
                  密码
                  <input type="password" {...user.register("password")} />
                </label>
              )}
            </>
          )}
          {action === "grant" && (
            <div className="grid">
              <label>
                数据库
                <input {...user.register("database")} />
              </label>
              <label>
                权限
                <select {...user.register("permission")}>
                  <option value="readonly">只读</option>
                  <option value="readwrite">读写</option>
                </select>
              </label>
            </div>
          )}
          {action === "drop" && (
            <label className="check">
              <input type="checkbox" {...user.register("confirm")} />
              确认删除角色；存在依赖对象时拒绝删除
            </label>
          )}
          <button>执行用户操作</button>
        </form>
        <details>
          <summary>删除数据库</summary>
          <p>不会强制断开活跃连接。输入要删除的数据库名：</p>
          <input
            value={confirmDB}
            onChange={(e) => setConfirmDB(e.target.value)}
          />
          <button
            className="danger"
            disabled={!confirmDB}
            onClick={() =>
              run(() =>
                api(
                  "/pg/databases/" + encodeURIComponent(confirmDB) + "/drop",
                  { confirm: true },
                ),
              )
            }
          >
            永久删除 {confirmDB}
          </button>
        </details>
      </section>
    </>
  );
}
