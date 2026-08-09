CREATE TABLE metadata (
    key TEXT PRIMARY KEY,
    value BLOB NOT NULL
);

CREATE TABLE repositories (
    id INTEGER PRIMARY KEY,
    root_path TEXT NOT NULL,
    home_path TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    UNIQUE (root_path, home_path)
);

CREATE UNIQUE INDEX repositories_one_default
ON repositories(home_path)
WHERE is_default = 1;

CREATE TABLE files (
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    target_path TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    source_path TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('ordinary', 'secret')),
    layer TEXT NOT NULL CHECK (layer IN ('base', 'darwin', 'linux')),
    baseline_content_hash BLOB NOT NULL CHECK (length(baseline_content_hash) = 32),
    baseline_source_hash BLOB NOT NULL CHECK (length(baseline_source_hash) = 32),
    executable_bits INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    applied_at TEXT NOT NULL,
    retired_at TEXT,
    PRIMARY KEY (repository_id, target_path)
);

CREATE INDEX files_by_scope
ON files(repository_id, group_name, status);

CREATE TABLE aliases (
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    alias_path TEXT NOT NULL,
    canonical_target_path TEXT NOT NULL,
    group_name TEXT NOT NULL DEFAULT '',
    layer TEXT NOT NULL CHECK (layer IN ('all', 'darwin', 'linux')),
    status TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    applied_at TEXT NOT NULL,
    retired_at TEXT,
    PRIMARY KEY (repository_id, alias_path)
);
