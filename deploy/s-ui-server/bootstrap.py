#!/usr/bin/env python3
"""Initialize private s-ui settings and optionally create an AnyTLS inbound.

The session API payloads are intentionally coupled to s-ui v1.5.4. install.sh
checks the panel and embedded sing-box versions before this module may write.
"""

from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import pathlib
import sqlite3
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from typing import Optional


def env(name: str, default: str = "") -> str:
    return os.environ.get(name, default).strip()


def env_bool(name: str, default: bool = False) -> bool:
    value = env(name)
    if not value:
        return default
    if value.lower() in {"1", "true", "yes", "on"}:
        return True
    if value.lower() in {"0", "false", "no", "off"}:
        return False
    raise ValueError(f"{name} must be true or false")


def env_port(name: str, default: int) -> int:
    value = int(env(name, str(default)))
    if not 1 <= value <= 65535:
        raise ValueError(f"{name} must be between 1 and 65535")
    return value


def configure_db(db_path: pathlib.Path) -> None:
    if not db_path.is_file():
        raise RuntimeError(f"s-ui database does not exist: {db_path}")

    settings = {
        "webListen": env("SUI_PANEL_BIND", "127.0.0.1"),
        "subListen": env("SUI_SUB_BIND", "127.0.0.1"),
        "timeLocation": env("TZ", "Asia/Tokyo"),
    }
    with sqlite3.connect(db_path) as connection:
        for key, value in settings.items():
            cursor = connection.execute(
                "UPDATE settings SET value = ? WHERE key = ?", (value, key)
            )
            if cursor.rowcount == 0:
                connection.execute(
                    "INSERT INTO settings(key, value) VALUES(?, ?)", (key, value)
                )
        connection.commit()


class SUIClient:
    def __init__(self, base_url: str, username: str, password: str) -> None:
        cookie_jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(cookie_jar)
        )
        self.base_url = base_url.rstrip("/") + "/"
        self.username = username
        self.password = password

    def request(
        self, method: str, endpoint: str, form: Optional[dict[str, str]] = None
    ) -> dict:
        data = None
        headers = {"X-Requested-With": "XMLHttpRequest"}
        if form is not None:
            data = urllib.parse.urlencode(form).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        request = urllib.request.Request(
            urllib.parse.urljoin(self.base_url, endpoint),
            data=data,
            headers=headers,
            method=method,
        )
        with self.opener.open(request, timeout=300) as response:
            result = json.load(response)
        if not result.get("success"):
            raise RuntimeError(result.get("msg") or f"s-ui API failed: {endpoint}")
        return result

    def wait_and_login(self) -> None:
        last_error: Optional[Exception] = None
        for _ in range(60):
            try:
                self.request(
                    "POST",
                    "api/login",
                    {"user": self.username, "pass": self.password},
                )
                return
            except (OSError, RuntimeError, urllib.error.URLError) as error:
                last_error = error
                time.sleep(2)
        raise RuntimeError(f"s-ui did not become ready: {last_error}")

    def load(self, object_name: str, object_id: Optional[int] = None) -> list[dict]:
        suffix = f"?id={object_id}" if object_id is not None else ""
        result = self.request("GET", f"api/{object_name}{suffix}")
        return result["obj"].get(object_name, [])

    def save(
        self,
        object_name: str,
        action: str,
        data: object,
        init_users: Optional[list[int]] = None,
    ) -> dict:
        form = {
            "object": object_name,
            "action": action,
            "data": json.dumps(data, separators=(",", ":")),
        }
        if init_users:
            form["initUsers"] = ",".join(str(user_id) for user_id in init_users)
        return self.request("POST", "api/save", form)


def find_named(items: list[dict], key: str, value: str) -> Optional[dict]:
    return next((item for item in items if item.get(key) == value), None)


def configure_anytls() -> str:
    domain = env("ANYTLS_DOMAIN")
    cf_token = env("CF_API_TOKEN")
    if not domain or domain == "jp.example.com":
        raise ValueError("set ANYTLS_DOMAIN in .env")
    if not cf_token or cf_token.startswith("replace-with-"):
        raise ValueError("set CF_API_TOKEN in .env")

    panel_port = env_port("SUI_PANEL_PORT", 2095)
    panel_path = env("SUI_PANEL_PATH", "/app/").strip("/")
    client = SUIClient(
        f"http://127.0.0.1:{panel_port}/{panel_path}/",
        env("SUI_ADMIN_USERNAME", "suiadmin"),
        env("SUI_ADMIN_PASSWORD"),
    )
    client.wait_and_login()

    # Fail before making changes if the frontend API no longer has the object
    # shapes used by v1.5.4. Individual save responses are checked as well.
    for object_name in ("tls", "clients", "inbounds"):
        objects = client.load(object_name)
        if not isinstance(objects, list):
            raise RuntimeError(f"unexpected s-ui API shape for {object_name}")

    tls_name = "anytls-acme"
    tls_items = client.load("tls")
    existing_tls = find_named(tls_items, "name", tls_name)
    dns_challenge = {
        "provider": "cloudflare",
        "api_token": cf_token,
    }
    zone_token = env("CF_ZONE_TOKEN")
    if zone_token:
        dns_challenge["zone_token"] = zone_token

    acme = {
        "domain": [domain],
        "data_directory": "/app/acme",
        "default_server_name": domain,
        "provider": "letsencrypt",
        "disable_http_challenge": True,
        "disable_tls_alpn_challenge": True,
        "dns01_challenge": dns_challenge,
    }
    email = env("ACME_EMAIL")
    if email:
        acme["email"] = email

    tls_data = {
        "id": int(existing_tls["id"]) if existing_tls else 0,
        "name": tls_name,
        "server": {
            "enabled": True,
            "server_name": domain,
            "min_version": "1.2",
            "max_version": "1.3",
            "acme": acme,
        },
        "client": {"server_name": domain},
    }
    client.save("tls", "edit" if existing_tls else "new", tls_data)
    tls_id = int(find_named(client.load("tls"), "name", tls_name)["id"])

    client_name = env("ANYTLS_CLIENT_NAME", "cliproxy")
    anytls_password = env("ANYTLS_PASSWORD")
    existing_client = find_named(client.load("clients"), "name", client_name)
    if existing_client:
        client_id = int(existing_client["id"])
        client_data = client.load("clients", client_id)[0]
        client_data.setdefault("config", {})
        client_data.setdefault("inbounds", [])
        client_data.setdefault("links", [])
    else:
        client_id = 0
        client_data = {
            "enable": True,
            "name": client_name,
            "config": {},
            "inbounds": [],
            "links": [],
            "volume": 0,
            "expiry": 0,
            "up": 0,
            "down": 0,
            "desc": "CLIProxyAPI egress",
            "group": "proxy-llm-api",
            "remark": "jp",
        }
    client_data["config"]["anytls"] = {
        "name": client_name,
        "password": anytls_password,
    }
    client.save("clients", "edit" if existing_client else "new", client_data)
    if not client_id:
        client_id = int(find_named(client.load("clients"), "name", client_name)["id"])

    inbound_tag = env("ANYTLS_TAG", "jp-anytls")
    inbound_port = env_port("ANYTLS_PORT", 443)
    existing_inbound = find_named(client.load("inbounds"), "tag", inbound_tag)
    inbound_id = int(existing_inbound["id"]) if existing_inbound else 0
    inbound_data = {
        "id": inbound_id,
        "type": "anytls",
        "tag": inbound_tag,
        "listen": "::",
        "listen_port": inbound_port,
        "tls_id": tls_id,
        "padding_scheme": [
            "stop=8",
            "0=30-30",
            "1=100-400",
            "2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000",
            "3=9-9,500-1000",
            "4=500-1000",
            "5=500-1000",
            "6=500-1000",
            "7=500-1000",
        ],
        "addrs": [
            {"server": domain, "server_port": inbound_port, "remark": "-jp"}
        ],
        "out_json": {},
    }
    client.save(
        "inbounds",
        "edit" if existing_inbound else "new",
        inbound_data,
        [client_id] if not existing_inbound else None,
    )
    inbound_id = int(find_named(client.load("inbounds"), "tag", inbound_tag)["id"])

    # Editing an existing deployment may need to repair the client association.
    full_client = client.load("clients", client_id)[0]
    inbound_ids = [int(item) for item in full_client.get("inbounds", [])]
    if inbound_id not in inbound_ids or full_client.get("config", {}).get(
        "anytls", {}
    ).get("password") != anytls_password:
        full_client.setdefault("config", {})["anytls"] = {
            "name": client_name,
            "password": anytls_password,
        }
        full_client["inbounds"] = sorted(set(inbound_ids + [inbound_id]))
        client.save("clients", "edit", full_client)

    final_client = client.load("clients", client_id)[0]
    for link in final_client.get("links", []):
        if link.get("type") == "local" and link.get("uri", "").startswith(
            "anytls://"
        ):
            return link["uri"]
    raise RuntimeError("s-ui saved AnyTLS but did not generate a client link")


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    db_parser = subparsers.add_parser("db")
    db_parser.add_argument("path", type=pathlib.Path)
    subparsers.add_parser("anytls")
    args = parser.parse_args()

    try:
        if args.command == "db":
            configure_db(args.path)
        elif args.command == "anytls":
            print(configure_anytls())
    except (OSError, RuntimeError, ValueError, urllib.error.URLError) as error:
        print(f"bootstrap failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
