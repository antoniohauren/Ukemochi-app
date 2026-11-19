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

```sh
go install github.com/a-h/templ/cmd/templ@latest
templ generate
./bin/tailwindcss -i web/input.css -o static/app.css --minify
ADMIN_PIN=1234 SESSION_KEY=$(openssl rand -hex 32) go run ./cmd/ukemochi
# open http://localhost:8080
```

### Environment variables

| Name          | Default        | Purpose                          |
|---------------|----------------|----------------------------------|
| `ADDR`        | `:8080`        | Listen address                   |
| `DB_PATH`     | `ukemochi.db`  | SQLite file                      |
| `ADMIN_PIN`   | (required)     | PIN for `/admin`                 |
| `SESSION_KEY` | (required)     | HMAC key for the admin cookie    |
| `TZ`          | `America/Sao_Paulo` | Day boundary for reports    |

## Documentation

- [Requirements](docs/requirements.md)
- [Order flows](docs/flows.md)
- [Data model](docs/data-model.md)
- [Architecture](docs/architecture.md)
- [Deployment](docs/deployment.md)
- [Decisions](docs/decisions.md)
- [Implementation plan](docs/plan.md)

The Tailwind standalone binary goes in `bin/` and is not committed. Download it from the [Tailwind releases](https://github.com/tailwindlabs/tailwindcss/releases).
