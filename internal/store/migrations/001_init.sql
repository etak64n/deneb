-- Settings that deneb creates for itself, such as the secret that signs tokens.
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- The administrators of the dashboard.
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

-- Signed-in browsers. The token itself is only in the cookie; this keeps its SHA-256.
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL
);

-- Keys of the API. The secret itself is shown once; this keeps its SHA-256.
CREATE TABLE api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  secret_hash TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER
);

CREATE TABLE videos (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT '',
  -- waiting (for its upload), queued, processing, ready, failed
  status TEXT NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  -- From 0 to 1 while the video is processing.
  progress REAL NOT NULL DEFAULT 0,
  source_size INTEGER NOT NULL DEFAULT 0,
  duration REAL NOT NULL DEFAULT 0,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  -- A JSON array of the renditions of the HLS stream.
  renditions TEXT NOT NULL DEFAULT '[]',
  -- none, queued, processing, ready, failed
  captions_status TEXT NOT NULL DEFAULT 'none',
  captions_language TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  ready_at INTEGER
);
CREATE INDEX videos_by_created ON videos (created_at DESC, id DESC);

-- The work that runs in the background: encoding, captions, and webhook deliveries.
CREATE TABLE jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  -- transcode, captions, webhook
  kind TEXT NOT NULL,
  -- The video of a transcode or captions job, or the delivery of a webhook job.
  ref TEXT NOT NULL,
  -- queued, running, done, failed
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  run_after INTEGER NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX jobs_by_queue ON jobs (status, kind, run_after, id);

CREATE TABLE webhooks (
  id TEXT PRIMARY KEY,
  url TEXT NOT NULL,
  secret TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE TABLE webhook_deliveries (
  id TEXT PRIMARY KEY,
  webhook_id TEXT NOT NULL REFERENCES webhooks (id) ON DELETE CASCADE,
  event TEXT NOT NULL,
  payload TEXT NOT NULL,
  -- pending, delivered, failed
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  response_status INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  delivered_at INTEGER
);
CREATE INDEX deliveries_by_webhook ON webhook_deliveries (webhook_id, created_at DESC);
