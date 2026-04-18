# Freebuff2API

[English](README.md) | [简体中文](README_zh.md)

Freebuff2API is an OpenAI-compatible proxy server for [Freebuff](https://freebuff.com). It translates standard OpenAI API requests into Freebuff's backend format, allowing you to use Freebuff's free models with any OpenAI-compatible client, SDK, or CLI tool.

## Features

- **OpenAI Compatible API** — Standard OpenAI endpoints; works with any compatible client out of the box.
- **Stealth Request Handling** — Dynamic, randomized client fingerprints that mimic official Freebuff SDK behavior.
- **Multi-Token Rotation** — Cycle through multiple auth tokens with automatic periodic rotation.
- **HTTP Proxy Support** — Route all outbound traffic through a configurable upstream proxy.
- **Built-in Web Console** — Complete Freebuff login in browser and copy `AUTH_TOKENS` directly.
- **Runtime Token Registration** — No prefilled `AUTH_TOKENS` required; tokens can be added from web login flow.

## Getting Auth Tokens

Freebuff2API requires one or more Freebuff **auth tokens**. The easiest way is to install the Freebuff CLI:

```bash
npm i -g freebuff
```

Run `freebuff` in your terminal — on first launch it will guide you through login.

After logging in, your token is saved to a local credentials file:

| OS | Credentials Path |
|---|---|
| Windows | `C:\Users\<username>\.config\manicode\credentials.json` |
| Linux / macOS | `~/.config/manicode/credentials.json` |

The file looks like:

```json
{
  "default": {
    "id": "user_10293847",
    "name": "Zhang San",
    "email": "zhangsan@example.com",
    "authToken": "fa82b5c1-e39d-4c7a-961f-d2b3c4e5f6a7",
    ...
  }
}
```

Only the `authToken` value is needed — copy it as your **AUTH_TOKENS**.

> **Tip:** Log in with multiple accounts and configure all their tokens for higher throughput.

## Configuration

Configuration is managed via a JSON file and/or environment variables. The JSON keys and environment variable names are identical. By default the app looks for `config.json` in the working directory; use `-config` to specify another path.

```json
{
  "LISTEN_ADDR": ":8080",
  "UPSTREAM_BASE_URL": "https://codebuff.com",
  "AUTH_TOKENS": ["eyJhb..."],
  "ROTATION_INTERVAL": "6h",
  "REQUEST_TIMEOUT": "15m",
  "STREAM_TIMEOUT": "20m",
  "API_KEYS": [],
  "HTTP_PROXY": "",
  "ADMIN_PASSWORD": "",
  "MODEL_ALIASES": {
    "gpt-4o": "google/gemini-3.1-pro-preview"
  },
  "POLICY": {
    "MAX_RETRIES": 2,
    "RETRY_BACKOFF_BASE": "500ms",
    "RETRY_BACKOFF_MAX": "6s",
    "PER_TOKEN_CONCURRENCY": 8,
    "HEALTH_CHECK_ENABLED": true,
    "HEALTH_CHECK_INTERVAL": "3m",
    "HEALTH_FAILURE_THRESHOLD": 3
  }
}
```

### Reference

| Key / Env Var | Description |
|---|---|
| `LISTEN_ADDR` | Proxy listen address (default `:8080`) |
| `UPSTREAM_BASE_URL` | Freebuff backend URL (default `https://codebuff.com`) |
| `AUTH_TOKENS` | Freebuff auth tokens (JSON array or comma-separated env var) |
| `ROTATION_INTERVAL` | Run rotation interval (default `6h`) |
| `REQUEST_TIMEOUT` | Upstream request timeout (default `15m`) |
| `STREAM_TIMEOUT` | Stream request timeout (default same as `REQUEST_TIMEOUT`) |
| `API_KEYS` | Client API keys for proxy auth (empty = open access) |
| `HTTP_PROXY` | HTTP proxy for outbound requests |
| `ADMIN_PASSWORD` | Web admin password (when set, web login APIs require admin sign-in) |
| `MODEL_ALIASES` | Alias mapping exposed in `/api/model-aliases` and applied on `/v1/chat/completions` |
| `POLICY` | Runtime policy defaults for retry/backoff, token concurrency, and health checks |

Environment variables override JSON values when both are set.

## Web UI + Login Integration

After startup, open `http://<host>:8080/` to use the built-in console.  
The core logic from `freebuff_login_and_print.py` is now integrated into service endpoints:

- Optional: set `ADMIN_PASSWORD` to require simple password login for web management
- `POST /api/login/session`: create a login session and return `login_url`
- `GET /api/login/status`: poll authorization status, return user/token data, and auto-register token into runtime pool

After authorization, the token is available immediately without restart; the page still shows `AUTH_TOKENS` export for compatibility.

Additional admin/runtime endpoints:

- `GET/PUT /api/policy`: read/update live retry/timeout/concurrency/health-check policy
- `GET/PUT /api/model-aliases`: read/update live model alias mapping
- `GET /metrics`: Prometheus-compatible metrics endpoint

## Deployment

### Zeabur (avoid redirect loops)

- Set service protocol/port type to **HTTP** in Zeabur. Let Zeabur handle external HTTPS termination.
- Do **not** enable any force-HTTPS redirect option/env (for example `FORCE_HTTPS`).
- Keep app listen address as `LISTEN_ADDR=:8080` (or your internal HTTP port).
- Access UI with `/` or `/ui` directly; `/ui` is handled without trailing-slash redirect to reduce proxy rewrite loop risks.

### Docker

Pre-built multi-arch images are available on GHCR (image path follows your repository owner/name automatically):

```bash
docker run -d --name Freebuff2API \
  -p 8080:8080 \
  -e ADMIN_PASSWORD="your-password" \
  ghcr.io/<your-github-owner>/freebuff2api:latest
```

Build from source:

```bash
docker build -t Freebuff2API .
docker run -d -p 8080:8080 -e ADMIN_PASSWORD="your-password" Freebuff2API
```

## GitHub Actions Docker Auto Build

The repository includes `.github/workflows/docker.yml` to auto-build and push multi-arch images (amd64/arm64) to GHCR on:

- push to `main`
- push tags matching `v*`
- manual `workflow_dispatch`

Published tags include `latest` (default branch), git tag, and commit SHA.

### Build from Source

**Requirements:** Go 1.23+

```bash
git clone https://github.com/Quorinex/Freebuff2API.git
cd Freebuff2API
go build -o Freebuff2API .
./Freebuff2API -config config.json
```

## Links

- [linux.do](https://linux.do)

## Disclaimer

This project has no official affiliation with OpenAI, Codebuff, or Freebuff. All related trademarks and copyrights belong to their respective owners.

All contents within this repository are provided solely for communication, experimentation, and learning, and do not constitute production-ready services or professional advice. This project is provided on an "As-Is" basis, and users must use it at their own risk. The author assumes no liability for any direct or indirect damages resulting from the use, modification, or distribution of this project, nor provides any warranties of any kind, express or implied.

## License

MIT
