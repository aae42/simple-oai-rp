-- migrate:up
-- Rename api_key to api_key_hash
-- Note: Existing plaintext keys will need to be rehashed manually or users recreated

-- SQLite doesn't support ALTER COLUMN RENAME, so we need to recreate the table
CREATE TABLE users_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    api_key_hash TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO users_new (id, username, api_key_hash, created_at)
SELECT id, username, api_key, created_at FROM users;

DROP INDEX IF EXISTS idx_users_api_key;
DROP TABLE users;

ALTER TABLE users_new RENAME TO users;

CREATE INDEX idx_users_api_key_hash ON users(api_key_hash);

-- migrate:down
CREATE TABLE users_old (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    api_key TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO users_old (id, username, api_key, created_at)
SELECT id, username, api_key_hash, created_at FROM users;

DROP INDEX IF EXISTS idx_users_api_key_hash;
DROP TABLE users;

ALTER TABLE users_old RENAME TO users;

CREATE INDEX idx_users_api_key ON users(api_key);
