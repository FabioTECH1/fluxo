"""Nonprivileged checks of the upgrade data-preservation oracle only."""
import sqlite3
import unittest
import tempfile
import time
from pathlib import Path

from arm64_candidate import upgrade_columns, await_predecessor_checks


class UpgradeColumnsTest(unittest.TestCase):
    def setUp(self):
        self.db=sqlite3.connect(':memory:')
        self.db.execute('CREATE TABLE sites(id INTEGER PRIMARY KEY,domain TEXT,legacy_value TEXT)')
        self.db.execute("INSERT INTO sites VALUES(1,'fixture.test','retained')")
        self.before=upgrade_columns(self.db,'sites')

    def tearDown(self):
        self.db.close()

    def selected(self,columns):
        return self.db.execute('SELECT '+','.join('"'+x+'"' for x in columns)+' FROM sites ORDER BY id').fetchall()

    def test_known_addition_keeps_every_original_value_in_the_oracle(self):
        before=self.selected(self.before)
        self.db.execute("ALTER TABLE sites ADD COLUMN github_disconnected_username TEXT NOT NULL DEFAULT ''")
        self.db.execute("ALTER TABLE sites ADD COLUMN github_disconnected_user_id INTEGER NOT NULL DEFAULT 0")
        after=upgrade_columns(self.db,'sites',self.before)
        self.assertEqual(self.before,after)
        self.assertEqual(before,self.selected(after))
        self.db.execute("UPDATE sites SET legacy_value='changed'")
        self.assertNotEqual(before,self.selected(after))

    def test_existing_marker_remains_an_original_value(self):
        self.db.execute("ALTER TABLE sites ADD COLUMN github_disconnected_username TEXT NOT NULL DEFAULT ''")
        self.db.execute("UPDATE sites SET github_disconnected_username='recorded-owner'")
        before=upgrade_columns(self.db,'sites')
        self.assertIn('github_disconnected_username',upgrade_columns(self.db,'sites',before))
        self.assertEqual('recorded-owner',self.selected(before)[0][-1])

    def test_unreviewed_schema_and_invented_identity_are_rejected(self):
        for ddl in (
            'ALTER TABLE sites ADD COLUMN arbitrary TEXT',
            "ALTER TABLE sites ADD COLUMN github_disconnected_username BLOB NOT NULL DEFAULT ''",
            "ALTER TABLE sites ADD COLUMN github_disconnected_username TEXT DEFAULT ''",
            "ALTER TABLE sites ADD COLUMN github_disconnected_username TEXT NOT NULL DEFAULT 'owner'",
            "ALTER TABLE sites ADD COLUMN github_disconnected_user_id TEXT NOT NULL DEFAULT '0'",
            "ALTER TABLE sites ADD COLUMN github_disconnected_user_id INTEGER NOT NULL DEFAULT 1",
            'ALTER TABLE sites DROP COLUMN legacy_value',
        ):
            with self.subTest(ddl=ddl):
                self.db.execute('SAVEPOINT changed')
                self.db.execute(ddl)
                with self.assertRaises(RuntimeError):upgrade_columns(self.db,'sites',self.before)
                self.db.execute('ROLLBACK TO changed')
                self.db.execute('RELEASE changed')
        self.db.execute("ALTER TABLE sites ADD COLUMN github_disconnected_username TEXT NOT NULL DEFAULT ''")
        self.db.execute("UPDATE sites SET github_disconnected_username='invented-owner'")
        with self.assertRaises(RuntimeError):upgrade_columns(self.db,'sites',self.before)


class PredecessorChecksTest(unittest.TestCase):
    def test_wait_is_read_only_and_requires_checks_to_finish(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'fixture.db'
            with sqlite3.connect(path) as db:
                db.execute('CREATE TABLE php_update_operations(kind TEXT,status TEXT)')
                db.execute("INSERT INTO php_update_operations VALUES('check','running')")
            before=path.read_bytes();report={}
            with self.assertRaisesRegex(RuntimeError,'deadline'):
                await_predecessor_checks(path,report,timeout=0)
            self.assertEqual(before,path.read_bytes())
            def finish(_):
                with sqlite3.connect(path) as db:
                    db.execute("UPDATE php_update_operations SET status='succeeded'")
            await_predecessor_checks(path,report,pause=finish)
            self.assertTrue(report['predecessor_startup_checks_finished'])

    def test_due_but_not_yet_enqueued_startup_check_is_not_ready(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'fixture.db'
            with sqlite3.connect(path) as db:
                db.execute('CREATE TABLE php_update_operations(kind TEXT,status TEXT)')
                db.execute('CREATE TABLE php_update_schedule(id INTEGER,next_check INTEGER)')
                db.execute('INSERT INTO php_update_schedule VALUES(1,0)')
            before=path.read_bytes()
            with self.assertRaisesRegex(RuntimeError,'deadline'):
                await_predecessor_checks(path,{},timeout=0)
            self.assertEqual(before,path.read_bytes())
            observed=[]
            def actual_completion(_):
                observed.append(True)
                with sqlite3.connect(path) as db:
                    db.execute("INSERT INTO php_update_operations VALUES('check','succeeded')")
                    db.execute('UPDATE php_update_schedule SET next_check=?',(int(time.time())+3600,))
            result={};await_predecessor_checks(path,result,pause=actual_completion)
            self.assertEqual(observed,[True])
            self.assertTrue(result['predecessor_startup_checks_finished'])

    def test_mutations_never_enter_the_automatic_wait(self):
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'fixture.db'
            with sqlite3.connect(path) as db:
                db.execute('CREATE TABLE php_update_operations(kind TEXT,status TEXT)')
                db.execute('CREATE TABLE node_update_operations(kind TEXT,status TEXT)')
                db.execute("INSERT INTO php_update_operations VALUES('check','running')")
                db.execute("INSERT INTO node_update_operations VALUES('apply','queued')")
            before=path.read_bytes()
            with self.assertRaisesRegex(RuntimeError,'active runtime maintenance'):
                await_predecessor_checks(path,{},pause=lambda _: self.fail('mutation entered wait'))
            self.assertEqual(before,path.read_bytes())


if __name__=='__main__':
    unittest.main()
