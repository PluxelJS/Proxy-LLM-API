#!/usr/bin/env python3
"""Single-service New API lifecycle; all mutable state stays outside the checkout."""
import argparse
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import sqlite3
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_IMAGE = 'docker.io/calciumion/new-api:v0.13.2'


def state_path():
    return Path(os.environ.get('NEW_API_STATE_DIR',
        str(Path(os.environ.get('XDG_STATE_HOME', str(Path.home() / '.local/state'))) / 'new-api-runtime'))).resolve()


def read_env(path):
    result = {}
    if not path.exists():
        return result
    for line in path.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        key, sep, value = line.partition('=')
        if not sep or not re.fullmatch(r'[A-Z][A-Z0-9_]*', key):
            raise ValueError('Invalid .env syntax; use KEY=value without shell expressions')
        result[key] = value
    return result


def write_env(path, values):
    for value in values.values():
        if any(c in value for c in "\n\r'"):
            raise ValueError('Configuration values cannot contain quotes or newlines')
    temp = path.with_name(path.name + '.tmp')
    with temp.open('x') as f:
        f.write(''.join(f"{k}={v}\n" for k, v in values.items()))
    temp.replace(path)


def initialize(state):
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    state.chmod(0o700)
    env_file = state / '.env'
    values = read_env(env_file)
    defaults = {'NEW_API_IMAGE': DEFAULT_IMAGE, 'NEW_API_BIND_ADDRESS': '127.0.0.1',
        'NEW_API_PORT': '23000', 'NEW_API_DATA_DIR': str(state / 'data'),
        'NEW_API_SESSION_SECRET': secrets.token_hex(32), 'TZ': 'Asia/Taipei',
        'NEW_API_ENGINE': os.environ.get('NEW_API_ENGINE', 'podman'),
        'NEW_API_PROJECT': os.environ.get('NEW_API_PROJECT', 'new-api-runtime')}
    changed = False
    for key, value in defaults.items():
        if key not in values:
            values[key] = value
            changed = True
    if not values['NEW_API_SESSION_SECRET']:
        raise ValueError('NEW_API_SESSION_SECRET must not be empty')
    data = Path(values['NEW_API_DATA_DIR'])
    if not data.is_absolute():
        raise ValueError('NEW_API_DATA_DIR must be absolute')
    if changed:
        write_env(env_file, values)
    env_file.chmod(0o600)
    data.mkdir(parents=True, exist_ok=True, mode=0o700)
    data.chmod(0o700)
    return values


def engine(values=None):
    value = os.environ.get('NEW_API_ENGINE', (values or {}).get('NEW_API_ENGINE', 'podman'))
    if value not in ('podman', 'docker'):
        raise ValueError('NEW_API_ENGINE must be podman or docker')
    return value


def project(values=None):
    value = os.environ.get('NEW_API_PROJECT', (values or {}).get('NEW_API_PROJECT', 'new-api-runtime'))
    if not re.fullmatch(r'[a-z0-9][a-z0-9_-]*', value):
        raise ValueError('Invalid NEW_API_PROJECT')
    return value


def compose(state, values, *args):
    env = os.environ.copy()
    # Pass parsed values literally, independent of shell or dotenv interpolation.
    env.update(values)
    selected_engine = engine(values)
    # Docker writes bind mounts as the host user; rootless Podman maps root to that user.
    env.setdefault('NEW_API_CONTAINER_USER',
        f'{os.getuid()}:{os.getgid()}' if selected_engine == 'docker' else '0:0')
    command = [selected_engine, 'compose']
    if selected_engine == 'podman':
        command += ['--in-pod=false']
        env.setdefault('PODMAN_COMPOSE_PROVIDER', 'podman-compose')
    command += ['-f', str(ROOT / 'docker-compose.yaml'), '-p', project(values), *args]
    subprocess.run(command, env=env, check=True)


def check(values, wait=False):
    host = values['NEW_API_BIND_ADDRESS']
    if host == '0.0.0.0':
        host = '127.0.0.1'
    if host == '::':
        host = '::1'
    if ':' in host:
        host = '[' + host + ']'
    url = f"http://{host}:{values['NEW_API_PORT']}/api/status"
    client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + (120 if wait else 0)
    while True:
        try:
            with client.open(url, timeout=5) as response:
                if json.load(response).get('success') is True:
                    print('OK new-api')
                    return
        except (OSError, ValueError):
            pass
        if time.monotonic() >= deadline:
            raise ValueError('New API health check failed; inspect logs')
        time.sleep(2)


def backup(state, values, destination):
    source = Path(values['NEW_API_DATA_DIR']) / 'new-api.db'
    if not source.is_file():
        raise ValueError('No SQLite database to back up; finish Web setup first')
    destination.mkdir(mode=0o700)  # Never overwrite a previous snapshot.
    try:
        with sqlite3.connect(source.as_uri() + '?mode=ro', uri=True) as src:
            with sqlite3.connect(destination / 'new-api.db') as dst:
                src.backup(dst)
                if dst.execute('PRAGMA quick_check').fetchone() != ('ok',):
                    raise ValueError('SQLite backup integrity check failed')
        shutil.copy2(state / '.env', destination / '.env')
        (destination / 'manifest.json').write_text(json.dumps({'image': values['NEW_API_IMAGE'],
            'created_at': int(time.time()), 'format': 1}, indent=2) + '\n')
    except BaseException:
        shutil.rmtree(destination)
        raise
    print('Backup saved:', destination)


def restore(state, source):
    # Restore only into a fresh state directory; existing state is never replaced.
    if state.exists() and any(state.iterdir()):
        raise ValueError('Restore requires an empty NEW_API_STATE_DIR')
    values = read_env(source / '.env')
    if not values.get('NEW_API_SESSION_SECRET') or not values.get('NEW_API_IMAGE'):
        raise ValueError('Backup configuration missing')
    with sqlite3.connect((source / 'new-api.db').as_uri() + '?mode=ro', uri=True) as db:
        if db.execute('PRAGMA quick_check').fetchone() != ('ok',):
            raise ValueError('Backup integrity check failed')
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    data = state / 'data'
    data.mkdir(mode=0o700)
    shutil.copyfile(source / 'new-api.db', data / 'new-api.db')
    values['NEW_API_DATA_DIR'] = str(data)
    write_env(state / '.env', values)
    print('Restored; review port and image in .env before starting')


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', nargs='?', default='status', choices=[
        'init', 'up', 'down', 'restart', 'pull', 'status', 'check', 'logs', 'config', 'backup', 'restore'])
    parser.add_argument('path', nargs='?', help='New backup directory, or backup to restore')
    args = parser.parse_args()
    if (args.command in ('backup', 'restore')) != bool(args.path):
        parser.error('Only backup and restore require a directory argument')
    state = state_path()
    if args.command == 'restore':
        restore(state, Path(args.path).resolve())
        return
    values = initialize(state)
    if args.command == 'init':
        print('Initialized:', state)
    elif args.command == 'backup':
        backup(state, values, Path(args.path).resolve())
    elif args.command in ('up', 'restart'):
        flags = ['--force-recreate'] if args.command == 'restart' else []
        compose(state, values, 'up', '-d', *flags, 'new-api')
        check(values, wait=True)
    elif args.command == 'check':
        check(values)
    elif args.command == 'status':
        compose(state, values, 'ps')
        check(values)
    elif args.command == 'config':
        compose(state, values, 'config', '--services')
    elif args.command == 'logs':
        compose(state, values, 'logs', '-f', '--tail=200', 'new-api')
    else:
        compose(state, values, args.command)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, sqlite3.Error, subprocess.CalledProcessError) as error:
        print(f'new-api-runtime: {error}', file=sys.stderr)
        sys.exit(1)
