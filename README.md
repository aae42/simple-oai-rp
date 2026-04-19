# Simple OpenAI Reverse Proxy

A lightweight Go web service that provides authentication for OpenAI compatible
APIs like [llama-server](https://github.com/ggml-org/llama.cpp/tree/master/tools/server).

## Features

- 🔐 **OpenAI-compatible API key authentication** - Secure your Open AI
  API-compatible server with standard Bearer token auth
- 🚀 **Simple deployment** - Single binary, SQLite database, minimal
  configuration
- 🔄 **Transparent proxying** - Forwards all requests to llama-server while
  adding authentication

### Running

Start the proxy with default settings:

```bash
./simple-oai-rp
```

The proxy will:

- Start on port `8081` (configurable via `SIMPLE_PORT` env var)
- Proxy to `http://localhost:8080` (configurable via `SIMPLE_OAI_API_SERVER_URL`
  env var)
- If an admin API key hash is not supplied,
  it will generate an admin API key and print it and it's hash to stdout
- Use SQLite database at `./data/main.db` (configurable via `SIMPLE_DATA_PATH`
  env var)

### Configuration

Configure the proxy using environment variables (see `.env.example`):

```bash
# Your OpenAI API compatible service's URL
export SIMPLE_OAI_API_SERVER_URL="http://localhost:8080"
# Port to listen on
export SIMPLE_PORT="8081"
# Admin API key hash (auto-generated if not set)
export SIMPLE_ADMIN_API_KEY_HASH='$argon2id$v=19$...'
# Data directory path
export SIMPLE_DATA_PATH="./data"

./simple-oai-rp
```

## Usage

### 1. Create a User (Admin)

Use the admin API key to create users:

```bash
curl -X POST http://localhost:8081/admin/users \
  -H "Authorization: Bearer $ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"username": "john"}'
```

Response:

```json
{
  "username": "john",
  "api_key": "sk-abc123...",
  "created_at": "2024-01-15T10:30:00Z"
}
```

**Save the API key** - it won't be shown again!

### 2. List Users (Admin)

```bash
curl http://localhost:8081/admin/users/list \
  -H "Authorization: Bearer $ADMIN_KEY"
```

### 3. Make Requests (User)

Use the generated API key to make requests to the proxied llama-server:

```bash
curl http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $USER_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## API Endpoints

### Admin Endpoints (require admin API key)

- `POST /admin/users` - Create a new user
  - Body: `{"username": "string"}`
  - Returns: `{"username": "string", "api_key": "string"}`

- `GET /admin/users/list` - List all users
  - Returns: Array of user objects

### Proxy Endpoints (require user API key)

- `/*` - All other paths are proxied to llama-server
  - Most commonly: `/v1/chat/completions`, `/v1/completions`, `/v1/models`, etc.

## Development

Have devbox.
Run `just`.

## Production Deployment

### Using systemd

Create `/etc/systemd/system/simple-oai-rp.service`:

```ini
[Unit]
Description=Simple OpenAI API Reverse Proxy
After=network.target

[Service]
Type=simple
User=youruser
WorkingDirectory=/opt/simple-oai-rp
Environment="SIMPLE_OAI_API_SERVER_URL=http://localhost:8080"
Environment="SIMPLE_PORT=8081"
Environment="SIMPLE_ADMIN_API_KEY_HASH=$argon2id$v=19$m=19456,t=2,p=1$your-hash-here"
Environment="SIMPLE_DATA_PATH=/var/lib/oai-proxy/data"
ExecStart=/opt/simple-oai-rp/simple-oai-rp
Restart=always

[Install]
WantedBy=multi-user.target
```

Before starting, run migrations:

```bash
cd /opt/simple-oai-rp
DATABASE_URL=sqlite:/var/lib/oai-proxy/data/main.db dbmate --migrations-dir ./db/migrations up
```

Enable and start:

```bash
sudo systemctl enable simple-oai-rp
sudo systemctl start simple-oai-rp
```
