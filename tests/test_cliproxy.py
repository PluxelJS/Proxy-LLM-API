import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import yaml

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import cliproxy


class CLIProxyTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.state = Path(self.temp.name) / 'state with spaces $literal'
        self.values = cliproxy.initialize(self.state)
        self.values.update(SINGBOX_NODE_URL='', CF_TUNNEL_TOKEN='')

    def test_empty_configuration_starts_direct(self):
        document = cliproxy.prepare(self.state, self.values)
        self.assertEqual(set(document['services']), {'cli-proxy-api'})
        self.assertIn('egress', document['services']['cli-proxy-api']['networks'])
        config = yaml.safe_load((self.state / 'settings/config.yaml').read_text())
        self.assertEqual(config['proxy-url'], 'direct')

    def test_mode_matrix_and_no_direct_network(self):
        for singbox, cloudflare in ((False, False), (True, False), (False, True), (True, True)):
            with self.subTest(singbox=singbox, cloudflare=cloudflare):
                self.values.update(SINGBOX_NODE_URL='socks5://node.test:1080' if singbox else '',
                                   CF_TUNNEL_TOKEN='example$token' if cloudflare else '')
                document = cliproxy.prepare(self.state, self.values)
                self.assertEqual(set(document['services']), {'cli-proxy-api'} |
                    ({'sing-box'} if singbox else set()) | ({'api-entry', 'cloudflared'} if cloudflare else set()))
                cli = document['services']['cli-proxy-api']
                config = yaml.safe_load((self.state / 'settings/config.yaml').read_text())
                if singbox:
                    self.assertEqual(cli['networks'], ['private', 'egress'])
                    self.assertTrue(document['networks']['private']['internal'])
                    self.assertEqual(config['proxy-url'], cliproxy.PROXY)
                    self.assertEqual(cli['environment']['HTTPS_PROXY'], cliproxy.PROXY)
                    sing = json.loads((self.state / 'generated/sing-box.json').read_text())
                    self.assertEqual([x['type'] for x in sing['outbounds']], ['socks'])
                    self.assertEqual(sing['route']['final'], 'proxy')
                else:
                    self.assertEqual(cli['networks'], ['private', 'egress'])
                    self.assertEqual(config['proxy-url'], 'direct')
                if cloudflare:
                    self.assertEqual(document['services']['cloudflared']['networks'], ['tunnel', 'cloud-egress'])
                    self.assertNotIn('private', document['services']['cloudflared']['networks'])
                    self.assertEqual(document['services']['api-entry']['networks'], ['private', 'tunnel'])
                    self.assertIn('example$$token', (self.state / 'compose.json').read_text())
                for filename in ('compose.json', 'settings/config.yaml'):
                    self.assertEqual((self.state / filename).stat().st_mode & 0o777, 0o600)

    def test_invalid_link_warns_without_leaking_and_allows_startup(self):
        self.values.update(SINGBOX_NODE_URL='secret-unsupported://sensitive', CF_TUNNEL_TOKEN='valid')
        with patch('sys.stderr', new_callable=io.StringIO) as stderr:
            document = cliproxy.prepare(self.state, self.values)
        self.assertIn('WARNING', stderr.getvalue())
        self.assertNotIn('sensitive', stderr.getvalue())
        self.assertNotIn('sing-box', document['services'])
        self.assertIn('cli-proxy-api', document['services'])

    def test_probe_checks_forwarding_and_failure_only_warns(self):
        self.values['SINGBOX_NODE_URL'] = 'socks5://node.test:1080'
        with patch.object(cliproxy, 'compose') as compose:
            cliproxy.probe_singbox(self.state, self.values)
        args = compose.call_args.args
        self.assertEqual(args[args.index('--proxy') + 1], cliproxy.PROXY)
        self.assertEqual(args[args.index('--noproxy') + 1], '')
        with patch.object(cliproxy, 'compose', side_effect=subprocess.CalledProcessError(1, 'secret')), \
             patch('sys.stderr', new_callable=io.StringIO) as stderr:
            cliproxy.probe_singbox(self.state, self.values)
        self.assertIn('WARNING', stderr.getvalue())
        self.assertNotIn('secret', stderr.getvalue())

    def test_writable_config_preserves_edits_and_reapplies_proxy(self):
        self.values['SINGBOX_NODE_URL'] = 'socks5://node.test:1080'
        source = self.state / 'settings/config.yaml'
        config = yaml.safe_load(source.read_text())
        config.update({'proxy-url': 'direct', 'codex-api-key': [{'api-key': 'test', 'proxy-url': 'none'}],
                       'openai-compatibility': [{'api-key-entries': [{'proxy-url': 'http://elsewhere:80'}]}]})
        source.write_text(yaml.safe_dump(config))
        original = source.read_text()
        document = cliproxy.prepare(self.state, self.values)
        generated = yaml.safe_load((self.state / 'settings/config.yaml').read_text())
        self.assertNotEqual(source.read_text(), original)
        self.assertEqual(generated['codex-api-key'][0]['proxy-url'], 'none')
        self.assertEqual(generated['openai-compatibility'][0]['api-key-entries'][0]['proxy-url'], 'http://elsewhere:80')
        self.assertFalse(document['services']['cli-proxy-api']['volumes'][0]['read_only'])
        self.assertEqual(document['services']['cli-proxy-api']['volumes'][0]['source'], str(self.state / 'settings'))

    def test_bad_yaml_error_does_not_print_credentials(self):
        self.values['CF_TUNNEL_TOKEN'] = 'token'
        (self.state / 'settings/config.yaml').write_text('secret: [sensitive: [')
        with self.assertRaises(ValueError) as error:
            cliproxy.prepare(self.state, self.values)
        self.assertNotIn('sensitive', str(error.exception))

    def test_config_listing_is_read_only_and_redacted(self):
        self.values.update(SINGBOX_NODE_URL='socks5://node.test:1080', CF_TUNNEL_TOKEN='secret-token')
        cliproxy.write_env(self.state / '.env', self.values)
        result = subprocess.run([sys.executable, str(ROOT / 'scripts/cliproxy.py'), 'config'],
            env=dict(os.environ, CLIPROXY_STATE_DIR=str(self.state)), capture_output=True, text=True, check=True)
        self.assertEqual(result.stdout.splitlines(), ['cli-proxy-api', 'sing-box', 'api-entry', 'cloudflared'])
        self.assertFalse((self.state / 'compose.json').exists())
        self.assertNotIn('secret-token', result.stdout + result.stderr)

    def test_stop_still_works_after_credentials_removed(self):
        self.values['CF_TUNNEL_TOKEN'] = 'token'
        cliproxy.prepare(self.state, self.values)
        self.values['CF_TUNNEL_TOKEN'] = ''
        cliproxy.write_env(self.state / '.env', self.values)
        with patch.dict(os.environ, {'CLIPROXY_STATE_DIR': str(self.state)}), \
             patch.object(sys, 'argv', ['cliproxy', 'down']), \
             patch.object(cliproxy, 'compose') as compose:
            cliproxy.main()
        self.assertEqual(compose.call_args.args[-1], 'down')

    def test_mode_change_stops_old_stack_before_starting_new_one(self):
        self.values['CF_TUNNEL_TOKEN'] = 'token'
        cliproxy.prepare(self.state, self.values)
        (self.state / 'deployment.json').write_text(json.dumps({
            'engine': self.values['CLIPROXY_ENGINE'], 'project': self.values['CLIPROXY_PROJECT'],
            'singbox': False, 'cloudflare': True}))
        self.values.update(SINGBOX_NODE_URL='socks5://node.test:1080', CF_TUNNEL_TOKEN='')
        cliproxy.write_env(self.state / '.env', self.values)
        with patch.dict(os.environ, {'CLIPROXY_STATE_DIR': str(self.state)}), \
             patch.object(sys, 'argv', ['cliproxy', 'up']), \
             patch.object(cliproxy, 'compose') as compose, \
             patch.object(cliproxy, 'check'), \
             patch.object(cliproxy, 'ensure_image'):
            cliproxy.main()
        self.assertEqual([call.args[2:] for call in compose.call_args_list], [('down',), ('up', '-d', 'sing-box'), ('up', '-d', 'cli-proxy-api')])
        document = json.loads((self.state / 'compose.json').read_text())
        self.assertEqual(set(document['services']), {'cli-proxy-api', 'sing-box'})
        self.assertEqual(document['services']['cli-proxy-api']['networks'], ['private', 'egress'])

    def test_official_source_build_and_pin(self):
        with patch.object(cliproxy.subprocess, 'run') as run:
            cliproxy.build(self.state, self.values)
        args = run.call_args.args[0]
        self.assertIn(f'CLIPROXY_REF={cliproxy.DEFAULT_REF}', args)
        self.assertIn(self.values['CLI_PROXY_IMAGE'], args)
        self.values['CLIPROXY_REF'] = 'main'
        with self.assertRaisesRegex(ValueError, 'commit SHA'):
            cliproxy.build(self.state, self.values)
        dockerfile = (ROOT / 'build/CLIProxyAPI.Dockerfile').read_text()
        self.assertIn(cliproxy.UPSTREAM, dockerfile)

    def test_management_key_is_private_stable_and_separate(self):
        key = (self.state / 'management-key').read_text().strip()
        self.assertGreaterEqual(len(key), 32)
        cliproxy.initialize(self.state)
        self.assertEqual((self.state / 'management-key').read_text().strip(), key)
        self.assertEqual((self.state / 'management-key').stat().st_mode & 0o777, 0o600)
        config = yaml.safe_load((self.state / 'settings/config.yaml').read_text())
        self.assertNotIn(key, config['api-keys'])
        self.values['CF_TUNNEL_TOKEN'] = 'token'
        document = cliproxy.prepare(self.state, self.values)
        cli = document['services']['cli-proxy-api']
        self.assertEqual(cli['environment']['MANAGEMENT_PASSWORD'], key)
        self.assertEqual(len(cli['ports']), 1)
        self.values['CLIPROXY_BIND_ADDRESS'] = '0.0.0.0'
        with self.assertRaisesRegex(ValueError, 'loopback'):
            cliproxy.prepare(self.state, self.values)

    def test_legacy_config_migration_preserves_accounts_and_api_keys(self):
        old = self.state / 'config.yaml'
        original = (self.state / 'settings/config.yaml').read_text()
        old.write_text(original)
        (self.state / 'settings/config.yaml').unlink()
        self.values['CLIPROXY_CODEX_CALLBACK_PORT'] = '1234'
        cliproxy.write_env(self.state / '.env', self.values)
        values = cliproxy.initialize(self.state)
        self.assertEqual((self.state / 'settings/config.yaml').read_text(), original)
        self.assertEqual((self.state / 'config.yaml.before-webui').read_text(), original)
        self.assertNotIn('CLIPROXY_CODEX_CALLBACK_PORT', values)
        self.assertFalse(old.exists())

    def test_missing_default_image_builds_automatically(self):
        with patch.object(cliproxy.subprocess, 'run') as run, patch.object(cliproxy, 'build') as build:
            run.return_value.returncode = 1
            cliproxy.ensure_image(self.state, self.values)
            build.assert_called_once_with(self.state, self.values)
            build.reset_mock()
            run.return_value.returncode = 0
            cliproxy.ensure_image(self.state, self.values)
            build.assert_not_called()
            self.values['CLI_PROXY_IMAGE'] = 'example/custom:tag'
            cliproxy.ensure_image(self.state, self.values)
            build.assert_not_called()

    def test_login_cli_removed(self):
        result = subprocess.run([sys.executable, str(ROOT / 'scripts/cliproxy.py'), 'login', 'codex'],
            capture_output=True, text=True)
        self.assertEqual(result.returncode, 2)

    def test_supported_node_links(self):
        for url, protocol in [('vless://uuid@node.test:443?security=tls', 'vless'),
                              ('trojan://password@node.test:443', 'trojan'),
                              ('hy2://password@node.test:443', 'hysteria2'),
                              ('tuic://uuid:password@node.test:443', 'tuic'),
                              ('ss://YWVzLTEyOC1nY206cGFzcw@node.test:443', 'shadowsocks'),
                              ('https://user:pass@node.test:443', 'http')]:
            with self.subTest(protocol=protocol):
                self.values['SINGBOX_NODE_URL'] = url
                outbound, _ = cliproxy.validate(self.values)
                self.assertEqual(outbound['type'], protocol)


if __name__ == '__main__':
    unittest.main()
