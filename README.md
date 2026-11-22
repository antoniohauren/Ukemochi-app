# Ukemochi

Order helper for a sushi restaurant. Waitresses record WhatsApp and in-house orders from their phones, print 58mm kitchen tickets, copy ready-to-send WhatsApp messages, calculate change, and close orders. The admin sees daily reports by payment method.

- UI language: pt-BR
- Mobile first, installable (PWA)
- Minimal third-party code

## Stack

| Layer    | Choice                                   |
|----------|------------------------------------------|
| Server   | Go, stdlib `net/http`                    |
| Views    | [templ](https://templ.guide)             |
| Interac. | htmx (vendored in `static/`)             |
| CSS      | Tailwind CSS v4, standalone CLI (no Node) |
| DB       | SQLite via `modernc.org/sqlite` (no cgo) |
| TLS      | Caddy + Cloudflare DNS-01                |

## Quick start

Needs Go 1.27.4+, `curl` and `sha256sum`.

```sh
cp .env.example .env   # then set ADMIN_PIN and SESSION_KEY
make seed              # optional: the app already seeds a new database
make run
# open http://127.0.0.1:8080
```

The app reads `.env` from the folder its binary is in, then from the working directory, so a built binary only needs a `.env` beside it. Variables already set in the environment win. `make run` and `make seed` also load `.env` from the project root, and there its values override the shell. It is not committed.

`make run` runs `make gen` first: templ generate plus Tailwind build. The first run downloads the Tailwind binary into `bin/` and htmx into `static/`, both checked by SHA256.

To change the port, set `ADDR` in `.env`, e.g. `ADDR=127.0.0.1:3000`. Use `ADDR=0.0.0.0:8080` to reach it from phones on the LAN.

| Command            | What it does                              |
|--------------------|-------------------------------------------|
| `make run`         | Generate, then run the server             |
| `make test`        | Generate, then `go test ./...`            |
| `make seed`        | Load the sample menu into an empty menu (the app does this itself for a new database) |
| `make build-arm64` | Static build for the Raspberry Pi         |
| `make build-amd64` | Single `bin/ukemochi-amd64` for Linux x64 |
| `make build-windows` | Single `bin/ukemochi.exe` for Windows: no console window, a tray icon to open the app, open the log or quit; logs to `ukemochi.log` beside it |

### Environment variables

| Name          | Default        | Purpose                          |
|---------------|----------------|----------------------------------|
| `ADDR`        | `127.0.0.1:8080` | Listen address (host:port)     |
| `DB_PATH`     | `ukemochi.db`  | SQLite file                      |
| `ADMIN_PIN`   | (required)     | PIN for `/admin`, 6+ characters  |
| `SESSION_KEY` | (required)     | HMAC key for the admin cookie    |
| `TZ`          | `America/Sao_Paulo` | Day boundary for reports    |
| `LOG_PATH`    | (none)         | Also write the log to this file. The Windows build, which has no console, defaults to `ukemochi.log` beside the exe |

## Documentation

- [Requirements](docs/requirements.md)
- [Order flows](docs/flows.md)
- [Data model](docs/data-model.md)
- [Architecture](docs/architecture.md)
- [Deployment](docs/deployment.md)
- [Decisions](docs/decisions.md)
- [Implementation plan](docs/plan.md)
