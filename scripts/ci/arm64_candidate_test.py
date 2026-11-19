"""Nonprivileged checks of the upgrade data-preservation oracle only."""
import sqlite3
import unittest

from arm64_candidate import upgrade_columns


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


if __name__=='__main__':
    unittest.main()
