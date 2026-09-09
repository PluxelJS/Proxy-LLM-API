#!/usr/bin/env python3
"""Web-managed CLIProxyAPI with optional sing-box forwarding checks."""
import argparse
import copy
import fcntl
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import tempfile
import time
import urllib.request

import yaml

from runtime import read_env, write_env
from singbox import parse_node_url

ROOT = Path(__file__).resolve().parents[1]
UPSTREAM = 'https://github.com/router-for-me/CLIProxyAPI.git'
DEFAULT_REF = '7fac6b15bcfe5ea55c18c9eaec8e5b7e6457d974'
PROXY = 'socks5h://sing-box:1080'

API_ENTRY_CONFIG = """pid /tmp/nginx.pid;
error_log /dev/stderr warn;
worker_processes auto;
events { worker_connections 1024; }
http {
    access_log off;
    client_body_temp_path /tmp/client_body;
    proxy_temp_path /tmp/proxy;
    fastcgi_temp_path /tmp/fastcgi;
    uwsgi_temp_path /tmp/uwsgi;
    scgi_temp_path /tmp/scgi;
    map $http_upgrade $connection_upgrade { default upgrade; '' close; }
    server {
        listen 8080;
        client_max_body_size 100m;
        location = /healthz { return 200 'ok'; }
        location ~ ^/(v1|v1beta)/ {
            proxy_pass http://cli-proxy-api:8317;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection $connection_upgrade;
            proxy_buffering off;
            proxy_request_buffering off;
            proxy_read_timeout 3600s;
        }
        location / { return 404; }
    }
}
"""



def state_path():
    return Path(os.environ.get('CLIPROXY_STATE_DIR', str(Path(os.environ.get(
        'XDG_STATE_HOME', str(Path.home() / '.local/state'))) / 'cliproxy-runtime'))).resolve()


def private_write(path, text):
    fd, name = tempfile.mkstemp(prefix='.' + path.name, dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as output:
            output.write(text)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def initialize(state):
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    state.chmod(0o700)
    values = read_env(state / '.env')
    defaults = {
        'CLIPROXY_ENGINE': 'podman', 'CLIPROXY_PROJECT': 'cliproxy-runtime',
        'CLI_PROXY_IMAGE': 'localhost/proxy-llm-api/cli-proxy-api:local',
        'CLIPROXY_REF': DEFAULT_REF, 'CLIPROXY_BIND_ADDRESS': '127.0.0.1',
        'CLIPROXY_PORT': '8317',
        'SINGBOX_NODE_URL': '', 'CF_TUNNEL_TOKEN': '',
        'CLIPROXY_EXTERNAL_NETWORK': '',
        'SINGBOX_CHECK_URL': 'https://www.gstatic.com/generate_204',
        'SINGBOX_IMAGE': 'ghcr.io/sagernet/sing-box:v1.13.16',
        'CLOUDFLARED_IMAGE': 'docker.io/cloudflare/cloudflared:2026.7.3',
        'API_ENTRY_IMAGE': 'docker.io/library/nginx:1.30.0-alpine',
        'TZ': 'Asia/Taipei',
    }
    for key, default in defaults.items():
        values.setdefault(key, os.environ.get(key, default))
    for key in list(values):
        if key.startswith('CLIPROXY_') and key.endswith('_CALLBACK_PORT'):
            values.pop(key)
    if values != read_env(state / '.env'):
        write_env(state / '.env', values)
    (state / '.env').chmod(0o600)
    for folder in ('auth', 'logs', 'generated', 'settings'):
        (state / folder).mkdir(exist_ok=True, mode=0o700)
        (state / folder).chmod(0o700)
    key = state / 'management-key'
    if not key.exists():
        private_write(key, secrets.token_urlsafe(32) + '\n')
    key.chmod(0o600)
    if not key.read_text().strip():
        raise ValueError('management-key must not be empty')
    config = state / 'settings/config.yaml'
    legacy = state / 'config.yaml'
    if not config.exists() and legacy.is_file():
        private_write(config, legacy.read_text())
        legacy.rename(state / 'config.yaml.before-webui')
    if not config.exists():
        private_write(config, yaml.safe_dump({
            'host': '', 'port': 8317, 'auth-dir': '/data/auth',
            'api-keys': [secrets.token_urlsafe(32)], 'ws-auth': True,
            'remote-management': {'allow-remote': True, 'secret-key': '',
                                  'disable-control-panel': False, 'disable-auto-update-panel': True},
            'logging-to-file': False, 'usage-statistics-enabled': True,
            'proxy-url': 'direct',
        }, sort_keys=False))
    return values


def validate(values):
    if values['CLIPROXY_ENGINE'] not in ('docker', 'podman'):
        raise ValueError('CLIPROXY_ENGINE must be docker or podman')
    if not re.fullmatch(r'[a-z0-9][a-z0-9_-]*', values['CLIPROXY_PROJECT']):
        raise ValueError('Invalid CLIPROXY_PROJECT')
    try:
        if not ipaddress.ip_address(values['CLIPROXY_BIND_ADDRESS']).is_loopback:
            raise ValueError()
        if not 1 <= int(values['CLIPROXY_PORT']) <= 65535:
            raise ValueError()
    except ValueError:
        raise ValueError('CLIPROXY_BIND_ADDRESS must be loopback and CLIPROXY_PORT must be valid') from None
    singbox = bool(values['SINGBOX_NODE_URL'].strip())
    cloudflare = bool(values['CF_TUNNEL_TOKEN'].strip())
    outbound = None
    if singbox:
        try:
            outbound = parse_node_url(values['SINGBOX_NODE_URL'].strip())
        except (ValueError, TypeError, AttributeError, KeyError):
            # Parser exceptions may contain credentials from the input URL.
            print('WARNING: invalid SINGBOX_NODE_URL; starting without sing-box.', file=sys.stderr)
    return outbound, cloudflare


def prepare(state, values):
    outbound, cloudflare = validate(values)
    try:
        config = yaml.safe_load((state / 'settings/config.yaml').read_text())
    except yaml.YAMLError:
        raise ValueError('Invalid config.yaml; expected CLIProxyAPI YAML configuration') from None
    if not isinstance(config, dict):
        raise ValueError('config.yaml must be a mapping')
    if not isinstance(config.get('api-keys'), list) or not config['api-keys'] or any(
            not isinstance(key, str) or not key.strip() for key in config['api-keys']):
        raise ValueError('config.yaml must contain nonempty api-keys')
    config.update({'host': '', 'port': 8317, 'auth-dir': '/data/auth'})
    # Preserve Web UI edits in one writable source; reapply deployment invariants at startup.
    management = config.setdefault('remote-management', {})
    if not isinstance(management, dict):
        raise ValueError('remote-management must be a mapping')
    management.update({'allow-remote': True, 'disable-control-panel': False,
                       'disable-auto-update-panel': True})
    if outbound:
        config['proxy-url'] = PROXY
    elif config.get('proxy-url') == PROXY:
        config['proxy-url'] = 'direct'
    generated = state / 'generated'
    config_text = yaml.safe_dump(config, sort_keys=False)
    private_write(state / 'settings/config.yaml', config_text)

    def mount(source, target, readonly=False):
        return {'type': 'bind', 'source': str(source), 'target': target, 'read_only': readonly}

    user = f'{os.getuid()}:{os.getgid()}' if values['CLIPROXY_ENGINE'] == 'docker' else '0:0'
    common = {'restart': 'unless-stopped', 'logging': {'options': {'max-size': '16m'}},
              'cap_drop': ['ALL'], 'security_opt': ['no-new-privileges:true']}
    address = values['CLIPROXY_BIND_ADDRESS']
    if ':' in address:
        address = f'[{address}]'
    cli = {**copy.deepcopy(common), 'image': values['CLI_PROXY_IMAGE'], 'user': user,
           'command': ['./CLIProxyAPI', '-config', '/data/config/config.yaml'],
           'ports': [f"{address}:{values['CLIPROXY_PORT']}:8317"],
           'volumes': [mount(state / 'settings', '/data/config'),
                       mount(state / 'auth', '/data/auth'), mount(state / 'logs', '/CLIProxyAPI/logs')],
           'environment': {'TZ': values['TZ'],
                           'MANAGEMENT_PASSWORD': (state / 'management-key').read_text().strip(),
                           'MANAGEMENT_STATIC_PATH': '/CLIProxyAPI/static'},
           'labels': {'io.github.pluxeljs.config-sha256': hashlib.sha256(config_text.encode()).hexdigest()},
           'healthcheck': {'test': ['CMD', 'curl', '--fail', '--silent', 'http://127.0.0.1:8317/healthz'],
                           'interval': '10s', 'timeout': '5s', 'retries': 6, 'start_period': '15s'},
           'networks': ['private', 'egress']}
    services = {'cli-proxy-api': cli}
    networks = {'egress': {}, 'private': {'internal': True}}
    if outbound:
        networks['private'] = {'internal': True}
        sing_config = {'log': {'level': 'info'}, 'inbounds': [
            {'type': 'mixed', 'listen': '0.0.0.0', 'listen_port': 1080}],
            'outbounds': [outbound], 'route': {'final': 'proxy'}}
        sing_text = json.dumps(sing_config, indent=2) + '\n'
        private_write(generated / 'sing-box.json', sing_text)
        services['sing-box'] = {**copy.deepcopy(common), 'image': values['SINGBOX_IMAGE'],
            'user': user, 'command': ['run', '-c', '/etc/sing-box/config.json'],
            'volumes': [mount(generated / 'sing-box.json', '/etc/sing-box/config.json', True)],
            'networks': ['private', 'egress'],
            'labels': {'io.github.pluxeljs.config-sha256': hashlib.sha256(sing_text.encode()).hexdigest()},
            'healthcheck': {'test': ['CMD', 'sing-box', 'check', '-c', '/etc/sing-box/config.json'],
                            'interval': '10s', 'timeout': '5s', 'retries': 3}}
        for key in ('HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'http_proxy', 'https_proxy', 'all_proxy'):
            cli['environment'][key] = PROXY
        cli['environment'].update(NO_PROXY='localhost,127.0.0.1,::1', no_proxy='localhost,127.0.0.1,::1')
    if cloudflare:
        # cloudflared cannot reach the management listener: its origin is an API-only relay.
        networks.update({'tunnel': {'internal': True}, 'cloud-egress': {}})
        private_write(generated / 'nginx.conf', API_ENTRY_CONFIG)
        # No secrets in this file; the unprivileged relay needs read access.
        (generated / 'nginx.conf').chmod(0o644)
        services['api-entry'] = {**copy.deepcopy(common), 'image': values['API_ENTRY_IMAGE'],
            'user': '65534:65534', 'entrypoint': ['nginx'],
            'command': ['-c', '/etc/nginx/nginx.conf', '-g', 'daemon off;'],
            'volumes': [mount(generated / 'nginx.conf', '/etc/nginx/nginx.conf', True)],
            'networks': ['private', 'tunnel'],
            'labels': {'io.github.pluxeljs.backend': hashlib.sha256(
                (config_text + values['CLI_PROXY_IMAGE']).encode()).hexdigest()},
            'depends_on': {'cli-proxy-api': {'condition': 'service_healthy'}},
            'healthcheck': {'test': ['CMD', 'wget', '-q', '-O', '/dev/null', 'http://127.0.0.1:8080/healthz'],
                            'interval': '10s', 'timeout': '5s', 'retries': 3}}
        services['cloudflared'] = {**copy.deepcopy(common), 'image': values['CLOUDFLARED_IMAGE'],
            'environment': {'TUNNEL_TOKEN': values['CF_TUNNEL_TOKEN'].strip()},
            'command': ['tunnel', '--no-autoupdate', '--metrics', '0.0.0.0:2000', 'run'],
            'depends_on': {'api-entry': {'condition': 'service_healthy'}},
            'networks': ['tunnel', 'cloud-egress'],
            'healthcheck': {'test': ['CMD', 'cloudflared', 'tunnel', '--metrics', '127.0.0.1:2000', 'ready'],
                            'interval': '15s', 'timeout': '5s', 'retries': 5}}
    if values.get('CLIPROXY_EXTERNAL_NETWORK'):
        networks['integration'] = {'external': True, 'name': values['CLIPROXY_EXTERNAL_NETWORK']}
        cli['networks'].append('integration')
    document = {'services': services, 'networks': networks}
    # Compose interpolates dollars even in JSON; preserve literal tokens and paths.
    private_write(state / 'compose.json', json.dumps(document, indent=2).replace('$', '$$') + '\n')
    return document


def compose(state, values, *args, capture=False):
    command = [values['CLIPROXY_ENGINE'], 'compose']
    env = os.environ.copy()
    # Do not inherit ambient project/profile or alternate compose configuration.
    for key in list(env):
        if key.startswith('COMPOSE_'):
            env.pop(key)
    if values['CLIPROXY_ENGINE'] == 'podman':
        command += ['--in-pod=false']
        env.setdefault('PODMAN_COMPOSE_PROVIDER', 'podman-compose')
    command += ['-f', str(state / 'compose.json'), '-p', values['CLIPROXY_PROJECT'], *args]
    return subprocess.run(command, env=env, check=True, text=True, capture_output=capture)


def build(state, values):
    ref = values['CLIPROXY_REF']
    # A commit pin is required for repeatable local builds; no arbitrary repository.
    if not re.fullmatch(r'[a-f0-9]{40}', ref):
        raise ValueError('CLIPROXY_REF must be a full official upstream commit SHA')
    subprocess.run([values['CLIPROXY_ENGINE'], 'build', '-f', str(ROOT / 'build/CLIProxyAPI.Dockerfile'),
        '--build-arg', f'CLIPROXY_REF={ref}', '--build-arg', f'COMMIT={ref}',
        '--build-arg', f'VERSION=upstream-{ref[:12]}', '-t', values['CLI_PROXY_IMAGE'],
        str(ROOT / 'build')], check=True)


def ensure_image(state, values):
    if values['CLI_PROXY_IMAGE'] == 'localhost/proxy-llm-api/cli-proxy-api:local':
        image = subprocess.run([values['CLIPROXY_ENGINE'], 'image', 'inspect', values['CLI_PROXY_IMAGE']],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if image.returncode:
            print('Building the default CLIProxyAPI image from pinned official source...')
            build(state, values)


def probe_singbox(state, values):
    if not values.get('SINGBOX_NODE_URL', '').strip():
        return
    try:
        # An explicit SOCKS proxy and empty no-proxy list test actual forwarding, not just a port.
        compose(state, values, 'exec', '-T', 'cli-proxy-api', 'curl', '--fail', '--silent',
                '--show-error', '--connect-timeout', '5', '--max-time', '12',
                '--noproxy', '', '--proxy', PROXY, '--output', '/dev/null',
                values['SINGBOX_CHECK_URL'], capture=True)
        print('OK sing-box: test request forwarded through sing-box')
    except (OSError, subprocess.CalledProcessError):
        print('WARNING: sing-box forwarding check failed. CLIProxyAPI remains running; '
              'check the node or choose direct access in the management page.', file=sys.stderr)


def management_url(values):
    address = values['CLIPROXY_BIND_ADDRESS']
    if ':' in address:
        address = f'[{address}]'
    return f"http://{address}:{values['CLIPROXY_PORT']}/management.html"


def check(state, values, wait=False):
    address = values['CLIPROXY_BIND_ADDRESS']
    address = {'0.0.0.0': '127.0.0.1', '::': '::1'}.get(address, address)
    if ':' in address:
        address = f'[{address}]'
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + (120 if wait else 0)
    while True:
        try:
            with client.open(f"http://{address}:{values['CLIPROXY_PORT']}/healthz", timeout=5) as response:
                if response.status == 200:
                    print('OK cli-proxy-api')
                    probe_singbox(state, values)
                    return
        except OSError:
            pass
        if time.monotonic() >= deadline:
            raise ValueError('CLIProxyAPI health check failed; inspect logs')
        time.sleep(2)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', nargs='?', default='status', choices=[
        'init', 'build', 'up', 'restart', 'down', 'pull', 'status', 'check', 'logs', 'config', 'ui'])
    args = parser.parse_args()
    state = state_path()
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Serialize updates so startup never sees partially generated configuration.
    with (state / '.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        values = initialize(state)
        if args.command == 'init':
            print('Initialized:', state, '\nDirect access is enabled by default; SINGBOX_NODE_URL is optional.')
        elif args.command == 'ui':
            print('Management:', management_url(values))
            print('Management key:', (state / 'management-key').read_text().strip())
        elif args.command == 'build':
            if values['CLIPROXY_ENGINE'] not in ('podman', 'docker'):
                raise ValueError('CLIPROXY_ENGINE must be docker or podman')
            build(state, values)
        elif args.command == 'config':
            outbound, cloudflare = validate(values)
            print('cli-proxy-api')
            if outbound:
                print('sing-box')
            if cloudflare:
                print('api-entry\ncloudflared')
        elif args.command == 'pull':
            outbound, cloudflare = validate(values)
            images = [values['CLI_PROXY_IMAGE']]
            if outbound:
                images.append(values['SINGBOX_IMAGE'])
            if cloudflare:
                images.extend([values['API_ENTRY_IMAGE'], values['CLOUDFLARED_IMAGE']])
            for image in images:
                subprocess.run([values['CLIPROXY_ENGINE'], 'pull', image], check=True)
        elif args.command in ('down', 'status', 'logs', 'check'):
            # Use the last rendered deployment even if credentials were removed.
            if not (state / 'compose.json').exists():
                raise ValueError('No deployment yet; configure .env and run up first')
            previous = state / 'deployment.json'
            if previous.exists():
                old = json.loads(previous.read_text())
                values.update(CLIPROXY_ENGINE=old['engine'], CLIPROXY_PROJECT=old['project'])
            if args.command == 'check':
                check(state, values)
            else:
                command = {'status': ['ps'], 'logs': ['logs', '-f', '--tail=200']}.get(args.command, ['down'])
                # Logs must not hold a lock that prevents down/restart.
                if args.command == 'logs':
                    fcntl.flock(lock, fcntl.LOCK_UN)
                compose(state, values, *command)
        else:
            validate(values)
            ensure_image(state, values)
            previous = state / 'deployment.json'
            identity = {'schema': 3, 'engine': values['CLIPROXY_ENGINE'], 'project': values['CLIPROXY_PROJECT'],
                        'singbox': bool(values['SINGBOX_NODE_URL'].strip()),
                        'cloudflare': bool(values['CF_TUNNEL_TOKEN'].strip())}
            if previous.exists() and json.loads(previous.read_text()) != identity:
                # Avoid leaving an old direct-connected CLI running during a mode change.
                validate(values)
                old = json.loads(previous.read_text())
                old_values = dict(values, CLIPROXY_ENGINE=old['engine'], CLIPROXY_PROJECT=old['project'])
                compose(state, old_values, 'down')
            document = prepare(state, values)
            private_write(previous, json.dumps(identity) + '\n')
            flags = ['--force-recreate'] if args.command == 'restart' else []
            if 'sing-box' in document['services']:
                try:
                    compose(state, values, 'up', '-d', *flags, 'sing-box')
                except (OSError, subprocess.CalledProcessError):
                    print('WARNING: sing-box did not start; continuing CLIProxyAPI startup.', file=sys.stderr)
            services = [name for name in document['services'] if name != 'sing-box']
            compose(state, values, 'up', '-d', *flags, *services)
            check(state, values, wait=True)
            print('Management:', management_url(values))
            print('Management key file:', state / 'management-key')



if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        print(f'cliproxy-runtime: {error}', file=sys.stderr)
        sys.exit(1)
