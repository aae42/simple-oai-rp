-- migrate:up
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    api_key TEXT UNIQUE NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_api_key ON users(api_key);

-- migrate:down
DROP INDEX IF EXISTS idx_users_api_key;
DROP TABLE IF EXISTS users;
