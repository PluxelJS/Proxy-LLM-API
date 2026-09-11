import { useState } from "react";
import { Alert } from "@heroui/react/alert";
import { Button } from "@heroui/react/button";
import { Disclosure } from "@heroui/react/disclosure";
import { Fieldset } from "@heroui/react/fieldset";
import { Form } from "@heroui/react/form";
import { Surface } from "@heroui/react/surface";
import { Table } from "@heroui/react/table";
import { Toolbar } from "@heroui/react/toolbar";
import { useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { api, type Run } from "./api";
import {
  BooleanField,
  SelectField,
  TextInputField,
  type SelectOption,
} from "./Fields";

const userActions: SelectOption[] = [
  { id: "password", label: "重置密码" },
  { id: "create", label: "创建用户" },
  { id: "grant", label: "添加数据库权限" },
  { id: "enable", label: "启用登录" },
  { id: "disable", label: "禁用登录" },
  { id: "drop", label: "删除用户" },
];

const permissions: SelectOption[] = [
  { id: "readonly", label: "只读" },
  { id: "readwrite", label: "读写" },
];

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
  const [action, setAction] = useState("password");
  const [connection, setConnection] = useState("");
  const [confirmDB, setConfirmDB] = useState("");

  return (
    <>
      <section id="databases" className="management-section">
        <h2>数据库</h2>
        {pg.error && (
          <Alert status="danger">
            <Alert.Content>
              <Alert.Description>
                {pg.error.message}。请先在页面顶部启动 PostgreSQL。
              </Alert.Description>
            </Alert.Content>
          </Alert>
        )}
        <Table variant="secondary">
          <Table.ScrollContainer>
            <Table.Content aria-label="PostgreSQL 数据库">
              <Table.Header>
                <Table.Column isRowHeader>数据库</Table.Column>
                <Table.Column>Owner</Table.Column>
                <Table.Column>管理状态</Table.Column>
                <Table.Column>操作</Table.Column>
              </Table.Header>
              <Table.Body>
                {pg.data?.databases.map((database) => (
                  <Table.Row id={database.name} key={database.name}>
                    <Table.Cell>{database.name}</Table.Cell>
                    <Table.Cell>{database.owner}</Table.Cell>
                    <Table.Cell>
                      {database.managed ? "已登记" : "外部/系统"}
                    </Table.Cell>
                    <Table.Cell>
                      {database.managed && (
                        <Button
                          size="sm"
                          variant="secondary"
                          onPress={() =>
                            run(async () => {
                              const value = await api<{ url: string }>(
                                "/pg/connection",
                                {
                                  database: database.name,
                                  user: database.owner,
                                },
                              );
                              setConnection(value.url);
                              return { message: "连接串已显示，包含私有密码" };
                            })
                          }
                        >
                          显示连接串
                        </Button>
                      )}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>
        {connection && (
          <div>
            <pre>{connection}</pre>
            <Toolbar aria-label="连接串操作">
              <Button
                size="sm"
                variant="secondary"
                onPress={() =>
                  run(async () => {
                    await navigator.clipboard.writeText(connection);
                    return { message: "连接串已复制" };
                  })
                }
              >
                复制
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onPress={() => setConnection("")}
              >
                隐藏
              </Button>
            </Toolbar>
          </div>
        )}
        <Surface className="form-surface" variant="secondary">
          <Form
            onSubmit={create.handleSubmit((value) =>
              run(() => api("/pg/databases", value)),
            )}
          >
            <Fieldset>
              <Fieldset.Legend>新建数据库 + 登录用户</Fieldset.Legend>
              <Fieldset.Group>
                <div className="grid">
                  <TextInputField
                    label="数据库名"
                    placeholder="demo"
                    {...create.register("database", { required: true })}
                  />
                  <TextInputField
                    label="用户"
                    description="留空时生成数据库名_owner"
                    {...create.register("user")}
                  />
                </div>
                <BooleanField
                  label="选择已存在的用户"
                  isSelected={create.watch("existingUser")}
                  onChange={(value) =>
                    create.setValue("existingUser", value, {
                      shouldDirty: true,
                    })
                  }
                />
                <BooleanField
                  label="自动生成密码"
                  isSelected={create.watch("generatePassword")}
                  onChange={(value) =>
                    create.setValue("generatePassword", value, {
                      shouldDirty: true,
                    })
                  }
                />
                {!create.watch("generatePassword") && (
                  <TextInputField
                    label="密码"
                    type="password"
                    {...create.register("password")}
                  />
                )}
              </Fieldset.Group>
              <Fieldset.Actions>
                <Button type="submit" variant="primary">
                  创建并验证
                </Button>
              </Fieldset.Actions>
            </Fieldset>
          </Form>
        </Surface>
      </section>

      <section id="users" className="management-section">
        <h2>用户</h2>
        <Table variant="secondary">
          <Table.ScrollContainer>
            <Table.Content aria-label="PostgreSQL 用户">
              <Table.Header>
                <Table.Column isRowHeader>用户名</Table.Column>
                <Table.Column>登录</Table.Column>
                <Table.Column>密码</Table.Column>
              </Table.Header>
              <Table.Body>
                {pg.data?.users.map((account) => (
                  <Table.Row id={account.name} key={account.name}>
                    <Table.Cell>
                      {account.name}
                      {account.superuser ? " · 管理员" : ""}
                    </Table.Cell>
                    <Table.Cell>{account.login ? "允许" : "禁用"}</Table.Cell>
                    <Table.Cell>
                      {account.passwordKnown ? "工具已保存" : "未知 / 系统凭据"}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>
        <Surface className="form-surface" variant="secondary">
          <Form
            onSubmit={user.handleSubmit((value) =>
              run(() => api("/pg/users/" + action, value)),
            )}
          >
            <Fieldset>
              <Fieldset.Legend>用户操作</Fieldset.Legend>
              <Fieldset.Group>
                <div className="grid">
                  <TextInputField
                    label="用户名"
                    {...user.register("user", { required: true })}
                  />
                  <SelectField
                    label="操作"
                    value={action}
                    options={userActions}
                    onChange={setAction}
                  />
                </div>
                {["password", "create"].includes(action) && (
                  <>
                    <BooleanField
                      label="自动生成密码"
                      isSelected={user.watch("generatePassword")}
                      onChange={(value) =>
                        user.setValue("generatePassword", value, {
                          shouldDirty: true,
                        })
                      }
                    />
                    {!user.watch("generatePassword") && (
                      <TextInputField
                        label="密码"
                        type="password"
                        {...user.register("password")}
                      />
                    )}
                  </>
                )}
                {action === "grant" && (
                  <div className="grid">
                    <TextInputField
                      label="数据库"
                      {...user.register("database")}
                    />
                    <SelectField
                      label="权限"
                      value={user.watch("permission")}
                      options={permissions}
                      onChange={(value) =>
                        user.setValue("permission", value, {
                          shouldDirty: true,
                        })
                      }
                    />
                  </div>
                )}
                {action === "drop" && (
                  <BooleanField
                    label="确认删除角色"
                    description="存在依赖对象时拒绝删除"
                    isSelected={user.watch("confirm")}
                    onChange={(value) =>
                      user.setValue("confirm", value, { shouldDirty: true })
                    }
                  />
                )}
              </Fieldset.Group>
              <Fieldset.Actions>
                <Button type="submit" variant="primary">
                  执行用户操作
                </Button>
              </Fieldset.Actions>
            </Fieldset>
          </Form>
        </Surface>
        <Disclosure className="danger-disclosure">
          <Disclosure.Heading>
            <Disclosure.Trigger>
              删除数据库
              <Disclosure.Indicator />
            </Disclosure.Trigger>
          </Disclosure.Heading>
          <Disclosure.Content>
            <Disclosure.Body>
              <TextInputField
                label="数据库名"
                description="不会强制断开活跃连接"
                value={confirmDB}
                onChange={(event) => setConfirmDB(event.target.value)}
              />
              <Button
                variant="danger"
                isDisabled={!confirmDB}
                onPress={() =>
                  run(() =>
                    api(
                      "/pg/databases/" +
                        encodeURIComponent(confirmDB) +
                        "/drop",
                      { confirm: true },
                    ),
                  )
                }
              >
                永久删除 {confirmDB}
              </Button>
            </Disclosure.Body>
          </Disclosure.Content>
        </Disclosure>
      </section>
    </>
  );
}
