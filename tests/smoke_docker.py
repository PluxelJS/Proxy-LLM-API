"""Real Docker lifecycle and SQLite restore test; no upstream calls or credentials."""
import json
import os
from pathlib import Path
import secrets
import subprocess
import tempfile
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
PORT = 23003
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def api(path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(f'http://127.0.0.1:{PORT}'+path, data=data,
                                     headers={'Content-Type': 'application/json'})
    with client.open(request, timeout=15) as response:
        result = json.load(response)
    assert result['success'], path
    return result.get('data')


with tempfile.TemporaryDirectory(prefix='new-api-docker-') as temporary:
    root = Path(temporary)
    state = root / 'state with spaces'
    restored = root / 'restored'
    backup = root / 'backup'
    env = os.environ.copy()
    env.update(NEW_API_STATE_DIR=str(state), NEW_API_ENGINE='docker',
               NEW_API_PROJECT='new-api-docker-smoke')

    def run(*args):
        subprocess.run([str(ROOT / 'manage.sh'), *args], env=env, check=True)

    try:
        run('init')
        config = state / '.env'
        config.write_text(config.read_text().replace('NEW_API_PORT=23000', f'NEW_API_PORT={PORT}'))
        # All remaining operations must use the persisted Docker engine.
        env.pop('NEW_API_ENGINE')
        env.pop('NEW_API_PROJECT')
        run('up')
        password = secrets.token_hex(16)
        api('/api/setup', {'username': 'admin', 'password': password,
            'confirmPassword': password, 'SelfUseModeEnabled': True, 'DemoSiteEnabled': False})
        assert api('/api/setup')['status'] is True
        assert (state / 'data/new-api.db').stat().st_uid == os.getuid()
        run('restart')
        assert api('/api/setup')['status'] is True
        run('backup', str(backup))
        assert (backup / 'new-api.db').stat().st_mode & 0o777 == 0o600
        run('down')
        env['NEW_API_STATE_DIR'] = str(restored)
        run('restore', str(backup))
        run('up')
        assert api('/api/setup')['status'] is True
        print('Docker startup, persistent setup, restart, backup and restored startup passed')
    finally:
        for folder in (state, restored):
            if (folder / '.env').exists():
                env['NEW_API_STATE_DIR'] = str(folder)
                subprocess.run([str(ROOT / 'manage.sh'), 'down'], env=env, check=False)
