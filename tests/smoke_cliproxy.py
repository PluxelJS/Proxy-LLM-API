"""Real container test: web management, direct access and optional sing-box probes.

Uses a local fake HTTP CONNECT node, no real credentials or upstream API calls.
CLIPROXY_TEST_ENGINE=docker selects Docker; default is Podman.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import urllib.request
import urllib.error

import yaml

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import cliproxy

engine = os.environ.get('CLIPROXY_TEST_ENGINE', 'podman')
with tempfile.TemporaryDirectory(prefix='cliproxy-smoke-') as temp:
    state = Path(temp)
    values = cliproxy.initialize(state)
    values.update(CLIPROXY_ENGINE=engine, CLIPROXY_PROJECT=f'cliproxy-smoke-{os.getpid()}',
        CLI_PROXY_IMAGE=os.environ.get('CLIPROXY_TEST_IMAGE', 'localhost/proxy-llm-api/cli-proxy-api:local'),
        CLIPROXY_PORT='28317',
        SINGBOX_NODE_URL='http://fake-node:8080', SINGBOX_CHECK_URL='http://198.51.100.10/test', CF_TUNNEL_TOKEN='smoke-tunnel-token')
    config = yaml.safe_load((state / 'settings/config.yaml').read_text())
    config.update({'request-retry': 0, 'openai-compatibility': [{
        'name': 'smoke', 'base-url': 'http://198.51.100.10/v1',
        'api-key-entries': [{'api-key': 'fake-upstream-key'}],
        'models': [{'name': 'smoke', 'alias': 'smoke'}]}]})
    (state / 'settings/config.yaml').write_text(yaml.safe_dump(config))
    document = cliproxy.prepare(state, values)
    tunnel = document['services'].pop('cloudflared')
    document['services']['tunnel-client'] = {
        'image': 'docker.io/library/node:24-bookworm-slim', 'networks': tunnel['networks'],
        'command': ['node', '-e', 'setInterval(() => {}, 1000)'], 'stop_grace_period': '1s'}
    document['services']['fake-node'] = {
        'image': 'docker.io/library/node:24-bookworm-slim', 'networks': ['egress'],
        'stop_grace_period': '1s',
        'command': ['node', '-e', '''
const http = require('http');
http.createServer((req, res) => res.end('direct-node')).on('connect', (req, socket) => {
  socket.write('HTTP/1.1 200 Connection Established\\r\\n\\r\\n');
  socket.once('data', data => {
    const body = data.toString().startsWith('POST ') ? JSON.stringify({
      id: 'chatcmpl-smoke', object: 'chat.completion', created: 1, model: 'smoke',
      choices: [{index: 0, message: {role: 'assistant', content: 'via-node'}, finish_reason: 'stop'}],
      usage: {prompt_tokens: 1, completion_tokens: 1, total_tokens: 2}
    }) : 'via-node';
    socket.end('HTTP/1.1 200 OK\\r\\nContent-Type: application/json\\r\\nContent-Length: ' + Buffer.byteLength(body) + '\\r\\nConnection: close\\r\\n\\r\\n' + body);
  });
}).listen(8080, '0.0.0.0');
''']}
    cliproxy.private_write(state / 'compose.json', json.dumps(document))
    def compose(*args, capture=False):
        return cliproxy.compose(state, values, *args, capture=capture)
    try:
        compose('up', '-d')
        cliproxy.check(state, values, wait=True)
        client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        management_key = (state / 'management-key').read_text().strip()
        def management(path, body=None, method=None, key=management_key, content_type='application/json'):
            request = urllib.request.Request('http://127.0.0.1:28317/v0/management/' + path,
                data=body, method=method, headers={'Authorization': 'Bearer ' + key, 'Content-Type': content_type})
            return client.open(request, timeout=15)
        with client.open('http://127.0.0.1:28317/management.html', timeout=15) as page:
            html = page.read()
        assert len(html) > 10000 and b'<html' in html.lower(), 'Bundled management page is missing'
        try:
            management('config', key='wrong-key')
        except urllib.error.HTTPError as error:
            assert error.code == 401, error.code
        else:
            raise AssertionError('Management accepted a wrong key')
        with management('config') as response:
            assert response.status == 200
        # Exercise the same writable YAML endpoint used by the Web UI.
        with management('config.yaml') as response:
            updated = yaml.safe_load(response.read())
        updated['request-retry'] = 1
        with management('config.yaml', yaml.safe_dump(updated).encode(), 'PUT', content_type='application/yaml') as response:
            assert response.status == 200
        assert yaml.safe_load((state / 'settings/config.yaml').read_text())['request-retry'] == 1
        with management('auth-files?name=smoke-account.json',
                        json.dumps({'type': 'codex', 'email': 'smoke@example.invalid', 'access_token': 'fixture-only'}).encode(), 'POST') as response:
            assert response.status == 200
        assert (state / 'auth/smoke-account.json').is_file()
        # Re-run startup preparation: Web UI changes and accounts must survive a recreate.
        cliproxy.prepare(state, values)
        cliproxy.private_write(state / 'compose.json', json.dumps(document))
        compose('up', '-d', '--force-recreate')
        cliproxy.check(state, values, wait=True)
        with management('config.yaml') as response:
            assert yaml.safe_load(response.read())['request-retry'] == 1
        with management('auth-files') as response:
            assert 'smoke-account.json' in response.read().decode()
        with management('auth-files?name=smoke-account.json', method='DELETE') as response:
            assert response.status == 200
        assert not (state / 'auth/smoke-account.json').exists()
        # Cloudflared's network can only reach the API-only relay, never the management listener.
        def tunnel_status(path):
            program = "const h=require('http');h.get(" + json.dumps('http://api-entry:8080' + path) +                 ", r=>{console.log(r.statusCode);r.resume();}).on('error',()=>process.exit(1));"
            return compose('exec', '-T', 'tunnel-client', 'node', '-e', program, capture=True).stdout.strip()
        for path in ('/management.html', '/v0/management/config', '/v1/../v0/management/config',
                     '/v1/%2e%2e/v0/management/config'):
            assert tunnel_status(path) == '404', path
        assert tunnel_status('/v1/models') == '401', 'Relay must preserve API authentication'
        program = "require('http').get('http://cli-proxy-api:8317/management.html',r=>process.exit(1)).on('error',()=>process.exit(0)).setTimeout(2000,function(){this.destroy();});"
        compose('exec', '-T', 'tunnel-client', 'node', '-e', program)
        command = ['exec', '-T', 'cli-proxy-api', 'curl', '--silent', '--show-error', '--fail',
                   '--max-time', '5']
        # An unroutable reserved destination succeeds only through our fake node.
        proxy = compose(*command, '--proxy', cliproxy.PROXY, 'http://198.51.100.10/test', capture=True)
        assert proxy.stdout == 'via-node', proxy.stdout
        node_id = subprocess.check_output([engine, 'ps', '-q',
            '--filter', f"label=com.docker.compose.project={values['CLIPROXY_PROJECT']}",
            '--filter', 'label=com.docker.compose.service=fake-node'], text=True).strip()
        node_info = json.loads(subprocess.check_output([engine, 'inspect', node_id], text=True))[0]
        node_ip = next(iter(node_info['NetworkSettings']['Networks'].values()))['IPAddress']
        # Verify that direct egress remains available alongside the configured proxy.
        compose('exec', '-T', 'fake-node', 'node', '-e',
                f"require('http').get('http://{node_ip}:8080', r => {{ if(r.statusCode !== 200) process.exit(1); r.resume(); }}).on('error', () => process.exit(1))")
        direct = compose(*command, '--noproxy', '*', f'http://{node_ip}:8080/test', capture=True)
        assert direct.stdout == 'direct-node', 'Direct egress should remain available'
        client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        request = urllib.request.Request('http://127.0.0.1:28317/v1/chat/completions',
            data=json.dumps({'model': 'smoke', 'messages': [{'role': 'user', 'content': 'test'}]}).encode(),
            headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + config['api-keys'][0]})
        with client.open(request, timeout=15) as response:
            completion = json.load(response)
        assert completion['choices'][0]['message']['content'] == 'via-node', completion
        # An authenticated model request also traverses the public API relay.
        program = "const h=require('http');const r=h.request('http://api-entry:8080/v1/chat/completions'," + json.dumps({
            'method': 'POST', 'headers': {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + config['api-keys'][0]}}) +             ",res=>{let s='';res.on('data',d=>s+=d);res.on('end',()=>{if(res.statusCode!==200 || JSON.parse(s).choices[0].message.content!=='via-node')process.exit(1);});});r.on('error',()=>process.exit(1));r.end(" + json.dumps(json.dumps({'model':'smoke','messages':[{'role':'user','content':'test'}]})) + ");"
        compose('exec', '-T', 'tunnel-client', 'node', '-e', program)
        compose('stop', 'sing-box')
        # A failed proxy probe is a warning, not a service health failure.
        cliproxy.check(state, values)
        try:
            compose(*command, '--proxy', cliproxy.PROXY, 'http://198.51.100.10/test', capture=True)
        except subprocess.CalledProcessError:
            pass
        else:
            raise AssertionError('Proxy request succeeded after sing-box stopped')
        print('PASS: bundled Web UI, authenticated management, persistent config/accounts, API-only tunnel, direct egress, proxy forwarding and warning-only probe')
    except BaseException:
        compose('logs', '--tail=30', 'api-entry')
        raise
    finally:
        compose('down')
