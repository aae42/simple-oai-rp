# Simple OpenAI Reverse Proxy

A lightweight Go web service that provides authentication and request logging
for llama-server instances.
This proxy adds OpenAI-compatible API key authentication to llama-server,
which doesn't have built-in authentication.

## Features

- 🔐 **OpenAI-compatible API key authentication** - Secure your llama-server
  with standard Bearer token auth
- 👥 **User management** - Admin endpoint to create users and generate API keys
- 📊 **Request logging** - All requests are logged to SQLite with user info,
  IP addresses, and full request/response data
- 🚀 **Simple deployment** - Single binary, SQLite database, minimal
  configuration
- 🔄 **Transparent proxying** - Forwards all requests to llama-server while
  adding authentication

### Running

Start the proxy with default settings:

```bash
./simple-oi-rp
```

The proxy will:
- Start on port `8081` (configurable via `SIMPLE_PORT` env var)
- Proxy to `http://localhost:8080` (configurable via `SIMPLE_LLAMA_SERVER_URL` env var)
- Generate an admin API key and print it to stdout
- Use SQLite database at `./data/main.db` (configurable via `SIMPLE_DATA_PATH` env var)

### Configuration

Configure the proxy using environment variables (see `.env.example`):

```bash
export SIMPLE_LLAMA_SERVER_URL="http://localhost:8080"  # Your llama-server URL
export SIMPLE_PORT="8081"                                 # Port to listen on
export SIMPLE_ADMIN_API_KEY_HASH="$argon2id$v=19$..."    # Admin API key hash (auto-generated if not set)
export SIMPLE_DATA_PATH="./data"                          # Data directory path

./simple-oai-rp
```

## Usage

### 1. Create a User (Admin)

Use the admin API key to create users:

```bash
curl -X POST http://localhost:8081/admin/users \
  -H "Authorization: Bearer YOUR_ADMIN_API_KEY" \
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
  -H "Authorization: Bearer YOUR_ADMIN_API_KEY"
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
Description=Simple OpenAI Reverse Proxy for llama-server
After=network.target

[Service]
Type=simple
User=youruser
WorkingDirectory=/opt/oai-proxy
Environment="SIMPLE_LLAMA_SERVER_URL=http://localhost:8080"
Environment="SIMPLE_PORT=8081"
Environment="SIMPLE_ADMIN_API_KEY_HASH=$argon2id$v=19$m=19456,t=2,p=1$your-hash-here"
Environment="SIMPLE_DATA_PATH=/var/lib/oai-proxy/data"
ExecStart=/opt/oai-proxy/simple-oai-rp
Restart=always

[Install]
WantedBy=multi-user.target
```

Before starting, run migrations:
```bash
cd /opt/oai-proxy
DATABASE_URL=sqlite:/var/lib/oai-proxy/data/main.db dbmate --migrations-dir ./db/migrations up
```

Enable and start:

```bash
sudo systemctl enable simple-oai-rp
sudo systemctl start simple-oai-rp
```
