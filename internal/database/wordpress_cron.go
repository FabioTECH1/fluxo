package database

// WordPressCronSchema is additive base-only feature state. The cron row and
// existing execution journal remain the sole scheduled-job authority.
const WordPressCronSchema = `
CREATE TABLE IF NOT EXISTS wordpress_cron_settings (
 site_id INTEGER PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
 cron_id INTEGER NOT NULL UNIQUE,
 phase TEXT NOT NULL CHECK(phase IN ('enabling','enabled','disabling')),
 source_sha TEXT NOT NULL
);
`
