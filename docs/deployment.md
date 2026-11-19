# Deployment

The app runs on a computer inside the restaurant, such as a PC or a Raspberry Pi. Phones connect over the shop Wi-Fi. Orders keep working when the internet is down, but certificate renewal needs internet.

## Why HTTPS on a local network

Browsers install a PWA and allow `navigator.clipboard` only on a secure origin. `http://192.168.x.x` is not secure, so the app is served at `https://pedidos.<domain>` with a real certificate.

## 1. DNS (Cloudflare)

1. Give the server a fixed LAN IP. Set a DHCP reservation in the router, for example `192.168.0.10`.
2. In Cloudflare, add an `A` record `pedidos` pointing to `192.168.0.10`. Set it to **DNS only** (grey cloud), not proxied.
3. Create an API token with the **Zone:DNS:Edit** permission for that zone.

**Router DNS rebind protection:** some routers block DNS answers that point to private IPs. If `pedidos.<domain>` doesn't resolve on the Wi-Fi, allow the domain in the router, or point the phones to a DNS server such as `1.1.1.1`.

## 2. Caddy

Build Caddy with the Cloudflare DNS module:

```sh
xcaddy build --with github.com/caddy-dns/cloudflare
```

`deploy/Caddyfile`:

```
pedidos.example.com {
	tls {
		dns cloudflare {env.CLOUDFLARE_API_TOKEN}
	}
	reverse_proxy 127.0.0.1:8080
}
```

Caddy gets and renews the certificate with the DNS-01 challenge. No port needs to be open to the internet.

## 3. App service

`deploy/ukemochi.service`:

```ini
[Unit]
Description=Ukemochi
After=network.target

[Service]
User=ukemochi
WorkingDirectory=/var/lib/ukemochi
EnvironmentFile=/etc/ukemochi.env
ExecStart=/usr/local/bin/ukemochi
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`/etc/ukemochi.env` (mode `0600`):

```
ADDR=127.0.0.1:8080
DB_PATH=/var/lib/ukemochi/ukemochi.db
ADMIN_PIN=...
SESSION_KEY=...
TZ=America/Sao_Paulo
```

Build for a Raspberry Pi from another machine:

```sh
templ generate
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o ukemochi ./cmd/ukemochi
```

## 4. Backups

Run a daily online backup with cron. It is safe while the app is running:

```
0 4 * * * sqlite3 /var/lib/ukemochi/ukemochi.db ".backup '/var/backups/ukemochi-$(date +\%F).db'"
```

Copy the backups off the machine regularly, for example to a USB drive or cloud storage. If the SD card or disk fails, every order is lost.

## 5. Phones

1. Open `https://pedidos.<domain>` in Chrome.
2. Tap the menu, then **Instalar app**.
3. Set up the thermal printer in the Android print service and print a test ticket.
