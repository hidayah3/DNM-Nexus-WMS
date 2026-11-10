# DNM Nexus WMS — Deployment guide

This guide is for the team deploying the WMS to a Linux server. It works on any VPS or cloud machine that can run Docker.

## What gets deployed

`compose.yaml` runs four containers:

| Service | What it does |
|---|---|
| `db` | PostgreSQL 16. Holds orders, shipments, stock batches, movements and sync logs. |
| `wms-api` | Go API. Order sync, EasyParcel, inventory, Excel exports, webhooks. |
| `wms-web` | React frontend served by Nginx. |
| `caddy` | HTTPS (automatic certificates), staff login, and routing to the API and web. |

## Requirements

- Linux server with **Docker** and the **Docker Compose plugin** (`docker compose version` works).
- A domain name (e.g. `wms.example.com`) whose DNS **A record** points to the server.
- Ports **80** and **443** open to the internet. Caddy needs both to get the HTTPS certificate.
- Outbound HTTPS allowed, so the server can reach the K-Link order API and EasyParcel.

## 1. Get the code onto the server

```bash
sudo mkdir -p /opt/wms && sudo chown $USER /opt/wms
git clone https://github.com/hidayah3/DNM-Nexus-WMS.git /opt/wms
cd /opt/wms
```

All commands below run from the project folder (`/opt/wms`).

> **Before the first build,** fix these if they are still open, or the web build fails or the site shows a blank page: (1) `web/Dockerfile` needs `pnpm-lock.yaml` and `pnpm-workspace.yaml`, which `.gitignore` excludes; (2) some imports don't match the file-name capitals, which breaks Linux builds; (3) `compose.yaml` mounts `./web/dist` over the built site. Details are in the Known issues section of the WMS Technical Documentation.

## 2. Create `.env`

```bash
cp .env.deploy.example .env
nano .env
```

Fill in every `REPLACE_WITH_...` value. Generate the secrets on the server:

| Setting | How to generate |
|---|---|
| `POSTGRES_PASSWORD` | `openssl rand -hex 24` |
| `SMS_WMS_INGEST_TOKEN` | `openssl rand -hex 32` |
| `WMS_TOKEN_ENCRYPTION_KEY` | `openssl rand -base64 32` |
| `EASYPARCEL_WEBHOOK_TOKEN` | `openssl rand -hex 32` |
| `WMS_PREVIEW_PASSWORD_HASH` | see step 4 |

Set `WMS_DOMAIN` to your domain.

**Order sync (K-Link order API).** These settings make the WMS pull paid orders automatically:

```
ORDER_SYNC_URL=https://<k-link-api-host>/api/v1/webhook/wms/order/sync
ORDER_SYNC_AUTHORIZATION=Basic <base64 of username:password>
ORDER_SYNC_INTERVAL=5m
ORDER_SYNC_IMPORT_STATUSES=PAID
ORDER_SYNC_STOCKIST=WHMY
```

- Use the **production** K-Link URL. The `dcmy-api-dev-v2` host is for development only.
- To build the `Basic ...` value from a username and password: `echo -n 'username:password' | base64`, then put `Basic ` in front of the result.

**Warehouse sender address.** Fill in the `WAREHOUSE_SENDER_*` settings (name, phone, address, postcode, city and EasyParcel state code, e.g. `SGR`). Every new order uses this as the sender. If they are left empty, new orders copy the sender from the most recent order, so the first order would have none.

**EasyParcel.** Fill in `EASYPARCEL_CLIENT_ID` and `EASYPARCEL_CLIENT_SECRET` from the EasyParcel developer portal, and set `EASYPARCEL_ENVIRONMENT=production` when going live. Details are in `EASYPARCEL-READINESS.md`.

**SMS callbacks (optional).** `SMS_SHIPMENT_UPDATE_*` and `SMS_INVENTORY_SYNC_*` are the SMS endpoints that receive shipment and stock updates from the WMS. Leave them blank until those endpoints exist. Stock updates stay queued safely until then. See `SMS-INTEGRATION-CONTRACT.md`.

`.env` holds secrets. Never commit it or send it over chat.

## 3. Start the database and apply migrations

```bash
docker compose up -d db
```

Wait until `docker compose ps` shows `db` as `healthy`, then apply every migration in order:

```bash
for f in db/001_wms.sql db/002_inventory_lots_and_sms_sync.sql db/003_manual_orders.sql db/004_order_status.sql db/005_order_sync.sql db/006_fulfilment_courier_name.sql db/007_users_sessions.sql; do
  echo "== $f"
  docker compose exec -T db sh -c 'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "$f" || break
done
```

Every file should finish without `ERROR`. The migrations are safe to re-run.

## 4. Staff sign-in

Staff sign in on the WMS's own **Sign in** page; there is nothing to configure in Caddy. Accounts are stored in the database (`wms_user`, migration 007) with bcrypt-hashed passwords, and a signed-in session lasts 12 hours. Five wrong passwords for a username lock it for 15 minutes.

You create the accounts once the API is running (step 5). Run this for each staff member; it asks for the password, so it never appears in the command line or shell history:

```bash
read -rsp "Password: " PW; echo
printf '%s\n' "$PW" | docker compose exec -T wms-api /wms-api user add ahmad "Ahmad Ali"; unset PW
```

The last part (`"Ahmad Ali"`) is the name shown in the sidebar and audit trail; leave it out to use the username. Running the command again for an existing username **resets its password** and signs that person out everywhere. Passwords need at least 8 characters.

To disable an account (e.g. someone leaves):

```bash
docker compose exec db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "UPDATE wms_user SET active=false WHERE username='"'"'ahmad'"'"'; DELETE FROM wms_session WHERE username='"'"'ahmad'"'"';"'
```

Give every staff member their own account. Stock changes are recorded against the signed-in account, so shared accounts break the audit trail.

## 5. Build and start everything

```bash
docker compose up -d --build
docker compose ps
```

All four services should be `running` (and `db` `healthy`).

## 6. Check it works

```bash
curl -s https://<your-domain>/health
docker compose logs --tail=100 wms-api
```

In the API log, look for:

| Log line | Meaning |
|---|---|
| `WMS API listening on 0.0.0.0:8080` | API started |
| `order sync enabled; pulling orders every 5m0s` | Order sync is on |
| `order sync: N received, N new, ...` | Orders were imported |
| `order sync is not configured` | `ORDER_SYNC_URL` / `ORDER_SYNC_AUTHORIZATION` are missing |
| `refused the login (HTTP 401)` | Wrong `ORDER_SYNC_AUTHORIZATION` |
| `could not reach the order API` | Network/firewall or wrong URL |

Then in the browser:

1. Create your accounts (step 4), then open `https://<your-domain>`. You should see the **Sign in** page.
2. Go to **Orders** and click **⟳ Sync orders**. Paid orders appear.
3. The order API does not send delivery addresses, so each new order shows **Needs delivery details**. Click it and fill in the address and parcel size, then **Get live rates**.
4. Connect EasyParcel from the top bar (OAuth) before booking any shipment.

## Updating to a new version

```bash
cd /opt/wms
cp .env .env.backup-$(date +%F)
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' > wms-backup-$(date +%F).sql
git pull
# apply any new db/00X_*.sql files the same way as step 3
docker compose up -d --build
docker compose logs --tail=50 wms-api
```

Never replace `.env` with an example file. It holds the live secrets.

## Backups

Back up the database daily, e.g. with a cron job:

```bash
0 2 * * * cd /opt/wms && docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' | gzip > /opt/wms-backups/wms-$(date +\%F).sql.gz
```

Restore:

```bash
gunzip -c wms-YYYY-MM-DD.sql.gz | docker compose exec -T db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

Each call to the order API is also saved in the `order_sync_run` table, including the raw response. Orders can be recovered from there if an import ever fails.

## API routes

| Route | Purpose |
|---|---|
| `GET /api/orders` | List orders (`?from=YYYY-MM-DD&to=YYYY-MM-DD`) |
| `POST /api/orders/sync` | Pull orders from the order API now |
| `GET /api/orders/sync-status` | Whether order sync is configured, and the last result |
| `PUT /api/orders/{id}/shipment` | Save delivery address and parcel details |
| `PATCH /api/orders/{id}/fulfilment` | Save delivery / self-collection and courier choice |
| `PATCH /api/orders/{id}/status` | Cancel or complete an order |
| `POST /api/orders/{id}/courier-quotes` | Live EasyParcel rates |
| `GET /api/orders/{id}/commercial-invoice` | Data for the commercial invoice |
| `POST /api/delivery-batches/push` | Book shipments with EasyParcel (may charge the wallet) |
| `POST /api/delivery-batches/awb` | Refresh AWB / tracking |
| `POST /api/delivery-batches/sync-sms` | Send shipment updates to SMS |
| `POST /api/integrations/orders` | SMS pushes an order (bearer `SMS_WMS_INGEST_TOKEN`) |
| `GET /api/integrations/orders/{id}/shipment` | SMS reads shipment status |
| `POST /api/easyparcel/webhook/{token}` | EasyParcel shipment events |
| `GET /api/inventory`, `POST /api/inventory/receipts`, `POST /api/inventory/adjustments` | Stock |
| `GET /api/inventory/report`, `GET /api/inventory/movements?month=YYYY-MM` | Excel exports |
| `GET /health` | Health check |

## Before the first real shipment

- Only book with genuine, authorized orders. EasyParcel production bookings charge the wallet.
- Re-check that login is on (step 4) and that each staff member has their own account.
