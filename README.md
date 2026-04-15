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
- Start on port `8081` (configurable via `PORT` env var)
- Proxy to `http://localhost:8080` (configurable via `LLAMA_SERVER_URL` env var)
- Generate an admin API key and print it to stdout
- Create a SQLite database at `./proxy.db` (configurable via `DB_PATH` env var)

### Configuration

Configure the proxy using environment variables:

```bash
export LLAMA_SERVER_URL="http://localhost:8080"  # Your llama-server URL
export PORT="8081"                                # Port to listen on
export ADMIN_API_KEY="sk-admin-your-secret-key"  # Admin API key (auto-generated if not set)
export DB_PATH="./proxy.db"                       # SQLite database path

./simple-oi-rp
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
Environment="LLAMA_SERVER_URL=http://localhost:8080"
Environment="PORT=8081"
Environment="ADMIN_API_KEY=sk-your-admin-key"
Environment="DB_PATH=/var/lib/oai-proxy/proxy.db"
ExecStart=/opt/oai-proxy/proxy
Restart=always

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable simple-oai-rp
sudo systemctl start simple-oai-rp
```
