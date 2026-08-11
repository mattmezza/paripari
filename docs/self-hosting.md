# Self-Hosting PariPari

PariPari is designed to be self-hosted. It's a single Docker container with no external dependencies other than free APIs for exchange rates and gold prices.

## Quick start

### Docker Compose

```bash
git clone https://github.com/mattmezza/paripari.git
cd paripari
docker compose up -d
```

Visit `http://localhost:8080`. The app creates and manages its own SQLite database on first run.

### Customize the compose file

Edit `docker-compose.yml` to change:

```yaml
services:
  paripari:
    ports:
      - "8080:8080"  # Change to your desired port
    volumes:
      - ./data:/data  # Persist the database
    environment:
      BASE_URL: http://localhost:8080      # Your public URL
      SECURE_COOKIES: 0                    # 1 if behind HTTPS
      PORT: 8080
      DATA_DIR: /data
```

## SSL/TLS and reverse proxy

PariPari does not handle TLS directly. Put it behind a reverse proxy (nginx, Caddy, Traefik) that terminates SSL.

Example nginx configuration:

```nginx
server {
    listen 443 ssl http2;
    server_name paripari.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

**Important**: Set `SECURE_COOKIES=1` when your app is behind HTTPS. This ensures session cookies are marked as Secure and SameSite.

## Data and backups

### Database location

By default, the SQLite database and its write-ahead log (WAL) are stored in `./data/`:

```
./data/
  paripari.db      # Main database
  paripari.db-wal  # Write-ahead log
  paripari.db-shm  # Shared memory file (created when needed)
```

**Back up all three files** to ensure consistency.

### Backup strategy

Since PariPari uses SQLite, you have several backup options:

#### Option 1: Simple file copy (while the container is stopped)

```bash
docker compose stop paripari
tar -czf paripari-backup-$(date +%Y%m%d).tar.gz data/
docker compose start paripari
```

#### Option 2: Live backup with sqlite3

```bash
docker compose exec paripari sqlite3 /data/paripari.db ".backup /tmp/backup.db"
docker compose cp paripari:/tmp/backup.db ./backup.db
docker compose exec paripari rm /tmp/backup.db
```

#### Option 3: Automated daily backups

Add a cron job to your host:

```bash
0 2 * * * cd /path/to/paripari && docker compose exec -T paripari sqlite3 /data/paripari.db ".backup /tmp/backup.db" && docker compose cp paripari:/tmp/backup.db ./backups/paripari-$(date +\%Y\%m\%d).db && docker compose exec -T paripari rm /tmp/backup.db
```

### Recovery

To restore from a backup:

```bash
docker compose stop paripari
cp backups/paripari-YYYYMMDD.db data/paripari.db
rm -f data/paripari.db-wal data/paripari.db-shm  # Remove WAL files
docker compose start paripari
```

## Upgrading

### Check for updates

Latest releases are published at `ghcr.io/mattmezza/paripari`.

### Upgrade steps

```bash
# Pull the latest image
docker compose pull paripari

# Restart the container
docker compose up -d

# The app will run migrations automatically on startup
```

No manual schema changes needed—migrations are applied automatically.

### Rollback

If you need to revert to a previous version:

```bash
# Edit docker-compose.yml to pin a specific version
# Change:   image: ghcr.io/mattmezza/paripari:latest
# To:       image: ghcr.io/mattmezza/paripari:v0.1

docker compose pull paripari
docker compose up -d
```

Then restore your database from a backup if needed.

## External APIs

PariPari fetches data from two free APIs:

### Exchange rates (frankfurter.app)

- Fetched once per day automatically
- Cached in the database
- Covers CHF, EUR, TRY, USD, GBP, and more
- Offline fallback: uses cached rates if the API is unavailable
- Manual refresh available in Settings

### Gold spot price (gold-api.com)

- Fetched once per day automatically
- Cached in the database
- Price per gram in USD, converted using FX rates
- Offline fallback: app continues to work with cached prices
- Manual override available in Settings

If your deployment has no internet access, the app will work with cached data. Set manual values in Settings to update prices without internet.

## Monitoring

### Logs

```bash
docker compose logs -f paripari
```

Each request is logged with method, path, and duration:

```
paripari  | 2024-08-10 10:45:32 GET /dashboard 125ms
paripari  | 2024-08-10 10:45:33 POST /expenses 45ms
```

### Database health

The app includes automatic session cleanup (expired sessions deleted daily). No other maintenance needed.

### Health check (optional)

Add to your monitoring:

```bash
curl -s http://localhost:8080/health || echo "unhealthy"
```

(The `/health` endpoint returns 200 if the app is running.)

## Performance

PariPari is designed to be lightweight:

- Single Go binary (no runtime dependencies)
- Embedded CSS, JavaScript, templates (no separate assets to serve)
- SQLite is efficient for a household's data volume (typical: <1MB)
- Each request is logged; observe response times for insight

For a typical household:

- Dashboard: <50ms
- Expense changes: <100ms
- Scenario calculations: <200ms

If you notice slowness, check:

1. CPU/memory on the host
2. Disk I/O (SQLite depends on fsync reliability)
3. API latency (FX/gold price fetches are cached but can add latency on first call)

## Common issues

### Database locked error

If you see "database is locked" errors:

1. Ensure only one container is running (no duplicate compose instances)
2. Check for long-running background jobs (FX/gold updates)
3. Restart the container: `docker compose restart paripari`

### Missing exchange rates

If the app shows "exchange rates unavailable":

1. Check that the container has internet access
2. Check logs: `docker compose logs paripari`
3. Try a manual refresh in Settings
4. If offline, set a manual FX rate in Settings to proceed

### Cookies not persisting across browser sessions

Ensure `SECURE_COOKIES` is set correctly:

- `0` (default) for HTTP (localhost development)
- `1` for HTTPS (production behind reverse proxy)

If cookies still don't persist, check that your browser accepts third-party cookies.

## Advanced configuration

### Custom DATA_DIR

By default, the database is stored in `./data`. To use a different location:

```yaml
services:
  paripari:
    volumes:
      - /mnt/backup/paripari:/data  # Or any other path
    environment:
      DATA_DIR: /data
```

### Custom port and BASE_URL

```yaml
services:
  paripari:
    ports:
      - "9000:8080"
    environment:
      PORT: 8080
      BASE_URL: https://paripari.example.com
```

Note: `PORT` is the port inside the container; expose it externally via the `ports` directive.

### Disable PWA (for testing)

PWA features (installability, offline support) are always enabled. To disable service worker registration, set `DEV=1` (development mode only; not recommended for production).

## Security

- **Authentication**: All data requires login. Sessions are HTTP-only cookies.
- **No external data sharing**: FX/gold price fetches are read-only to public APIs.
- **SQLite in the container**: No external database to expose.
- **CSRF protection**: All forms include CSRF tokens; htmx requests validate the custom header.

Always run PariPari behind a reverse proxy with HTTPS in production, and set `SECURE_COOKIES=1`.

## Getting help

- Check logs: `docker compose logs paripari`
- Open an issue: [github.com/mattmezza/paripari/issues](https://github.com/mattmezza/paripari/issues)
- Review the architecture doc: [docs/architecture.md](architecture.md)
- **Hosting for more than your own household?** Read
  [docs/multi-tenancy.md](multi-tenancy.md) first — the data isolation is done
  and tested, but rate limiting, signup policy and resource limits are not.
