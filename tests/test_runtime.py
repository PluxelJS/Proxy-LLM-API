import importlib.util
import os
from pathlib import Path
import sqlite3
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('runtime', Path(__file__).parents[1] / 'scripts/runtime.py')
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class RuntimeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.state = self.root / 'state with spaces'
        old = os.umask(0o077)
        self.addCleanup(os.umask, old)

    def test_private_idempotent_state(self):
        first = runtime.initialize(self.state)
        first['NEW_API_PORT'] = '23002'
        runtime.write_env(self.state / '.env', first)
        second = runtime.initialize(self.state)
        self.assertEqual(first, second)
        self.assertEqual((self.state / '.env').stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.state.stat().st_mode & 0o777, 0o700)
        self.assertEqual(Path(first['NEW_API_DATA_DIR']).stat().st_mode & 0o777, 0o700)
        self.assertEqual(len(first['NEW_API_SESSION_SECRET']), 64)

    def test_backup_captures_wal_and_restore_preserves_usage(self):
        values = runtime.initialize(self.state)
        db = sqlite3.connect(Path(values['NEW_API_DATA_DIR']) / 'new-api.db')
        self.addCleanup(db.close)
        db.execute('PRAGMA journal_mode=WAL')
        db.execute('CREATE TABLE usage (tokens INTEGER)')
        db.execute('INSERT INTO usage VALUES (311)')
        db.commit()
        backup = self.root / 'backup'
        runtime.backup(self.state, values, backup)
        db.execute('INSERT INTO usage VALUES (500)')
        db.commit()
        restored = self.root / 'restored'
        runtime.restore(restored, backup)
        restored_values = runtime.initialize(restored)
        self.assertEqual(restored_values['NEW_API_SESSION_SECRET'], values['NEW_API_SESSION_SECRET'])
        self.assertEqual(restored_values['NEW_API_IMAGE'], values['NEW_API_IMAGE'])
        with sqlite3.connect(restored / 'data/new-api.db') as copy:
            self.assertEqual(copy.execute('SELECT sum(tokens) FROM usage').fetchone(), (311,))
        with self.assertRaises(ValueError):
            runtime.restore(restored, backup)
        with self.assertRaises(FileExistsError):
            runtime.backup(self.state, values, backup)

    def test_only_selected_engine_and_service(self):
        values = runtime.initialize(self.state)
        with patch.dict(os.environ, {'NEW_API_ENGINE': 'podman', 'NEW_API_PROJECT': 'runtime-test'}):
            with patch.object(runtime.subprocess, 'run') as run:
                runtime.compose(self.state, values, 'up', '-d', 'new-api')
                args = run.call_args.args[0]
                self.assertEqual(args[:3], ['podman', 'compose', '--in-pod=false'])
                self.assertEqual(args[-3:], ['up', '-d', 'new-api'])
                self.assertNotIn('--remove-orphans', args)
                self.assertNotIn(values['NEW_API_SESSION_SECRET'], ' '.join(args))
                self.assertEqual(run.call_args.kwargs['env']['NEW_API_DATA_DIR'], values['NEW_API_DATA_DIR'])
        with patch.dict(os.environ, {'NEW_API_ENGINE': 'docker'}):
            with patch.object(runtime.subprocess, 'run') as run:
                runtime.compose(self.state, values, 'config', '--services')
                self.assertNotIn('--in-pod=false', run.call_args.args[0])

    def test_bad_restore_does_not_create_destination(self):
        source = self.root / 'bad-backup'
        source.mkdir()
        with self.assertRaises(ValueError):
            runtime.restore(self.state, source)
        self.assertFalse(self.state.exists())


if __name__ == '__main__':
    unittest.main()
