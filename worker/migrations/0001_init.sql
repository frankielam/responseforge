CREATE TABLE IF NOT EXISTS parse_rules (
  id TEXT PRIMARY KEY,
  pattern TEXT NOT NULL UNIQUE,
  rule TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS raw_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  captured_at TEXT,
  url TEXT,
  method TEXT,
  status INTEGER,
  content_type TEXT,
  body TEXT,
  truncated INTEGER,
  error TEXT
);

CREATE TABLE IF NOT EXISTS rule_jobs (
  id TEXT PRIMARY KEY,
  pattern TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT,
  created_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS rule_jobs_one_running
  ON rule_jobs (pattern)
  WHERE status = 'running';
