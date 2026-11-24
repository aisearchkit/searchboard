package store

// migrations are applied in order. The database records how many have run in
// PRAGMA user_version. Never edit an applied migration: append a new one.
var migrations = []string{
	// 1: initial schema
	`
CREATE TABLE clusters (
  id                   TEXT PRIMARY KEY,
  name                 TEXT NOT NULL UNIQUE,
  url                  TEXT NOT NULL,
  username             TEXT NOT NULL DEFAULT '',
  password             TEXT NOT NULL DEFAULT '',
  env                  TEXT NOT NULL DEFAULT 'dev',
  insecure_skip_verify INTEGER NOT NULL DEFAULT 0,
  created_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE devtools_history (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  cluster_id TEXT NOT NULL,
  request    TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE migration_tasks (
  id         TEXT PRIMARY KEY,
  source_id  TEXT NOT NULL,
  target_id  TEXT NOT NULL,
  spec       TEXT NOT NULL,
  status     TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE inspection_reports (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  cluster_id TEXT NOT NULL,
  summary    TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`,
	// 2: DevTools history records what was run and how it ended
	`
ALTER TABLE devtools_history ADD COLUMN method      TEXT    NOT NULL DEFAULT '';
ALTER TABLE devtools_history ADD COLUMN path        TEXT    NOT NULL DEFAULT '';
ALTER TABLE devtools_history ADD COLUMN status      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE devtools_history ADD COLUMN risk        TEXT    NOT NULL DEFAULT 'safe';
ALTER TABLE devtools_history ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0;
CREATE INDEX devtools_history_cluster ON devtools_history (cluster_id, id DESC);
`,
}
