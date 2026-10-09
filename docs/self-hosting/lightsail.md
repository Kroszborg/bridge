# Deploying to AWS Lightsail

One Lightsail instance runs everything: PostgreSQL, the API, the worker, the dashboard, the website,
and Caddy in front for HTTPS. Hosted Bridge runs this way, in the Mumbai region. Images are built on your machine and shipped over SSH, so the server
never runs a build.

## 1. Create the instance

* **Blueprint:** Linux, Ubuntu 24.04 LTS
* **Plan:** General purpose, 2 GB memory ($12/month). Bridge uses about 400 MB; the rest is headroom.
* **Automatic snapshots:** on. The `pgdata` volume holds all data, including queued jobs.

Then, in the instance's **Networking** tab:

1. **Create static IP** and attach it, so DNS keeps working when the instance restarts.
2. Add a firewall rule for **HTTPS, TCP 443** (SSH 22 and HTTP 80 are open by default).

## 2. Prepare the server

```bash
ssh -i LightsailDefaultKey.pem ubuntu@<static-ip>

# 2 GB swap as a safety net
sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab

# Docker Engine and the Compose plugin: https://docs.docker.com/engine/install/ubuntu/
sudo usermod -aG docker ubuntu
sudo mkdir -p /opt/bridge && sudo chown ubuntu:ubuntu /opt/bridge
```

## 3. Point DNS at the server

Create three `A` records for the static IP, for example:

| Name | Serves |
| --- | --- |
| `bridge.example.com` | Website |
| `app.bridge.example.com` | Dashboard and status page |
| `api.bridge.example.com` | API, phones, webhooks |

No domain yet? `<ip-with-dashes>.sslip.io` names resolve to the IP they contain, for example
`app.203-0-113-10.sslip.io`, and Caddy gets real certificates for them.

## 4. Write `.env.production`

In the repository root (git ignores it):

```dotenv
BRIDGE_SITE_HOST=bridge.example.com
BRIDGE_APP_HOST=app.bridge.example.com
BRIDGE_API_HOST=api.bridge.example.com

BRIDGE_SITE_URL=https://bridge.example.com
BRIDGE_DASHBOARD_URL=https://app.bridge.example.com
BRIDGE_PUBLIC_URL=https://api.bridge.example.com

BRIDGE_ENV=production
BRIDGE_VERSION=prod
POSTGRES_PASSWORD=<openssl rand -hex 24>
BRIDGE_SECRET_KEY=<openssl rand -base64 32>
BRIDGE_ALLOW_SIGNUP=true
```

Back up `BRIDGE_SECRET_KEY`: saved provider credentials cannot be decrypted without it. Add
`BRIDGE_SMTP_*` and other settings from the [self-hosting guide](README.md) as needed.

## 5. Deploy

```bash
tools/deploy.sh ubuntu@<static-ip> LightsailDefaultKey.pem .env.production
```

The script builds the images, streams them to the server, copies the Compose files, the Caddyfile
and the env file to `/opt/bridge`, and starts the stack with `docker-compose.prod.yml`. Only Caddy
listens publicly (80 and 443); it obtains certificates on the first request to each hostname.

Run the same command to ship an update. Migrations run before the API starts.

For a private server, set `BRIDGE_ALLOW_SIGNUP=false` after creating your account and deploy
again; teammates join through invite links. Leave it `true` only for a public service like hosted
Bridge.

## Operating

```bash
ssh ubuntu@<static-ip>
cd /opt/bridge
alias dc='docker compose -f docker-compose.yml -f docker-compose.prod.yml'
dc ps                 # status
dc logs -f api        # follow a service
dc exec postgres pg_dump -U bridge bridge | gzip > backup-$(date +%F).sql.gz
```

Changing hostnames: update `.env.production` and deploy again. The website bakes its URLs in at
build time, so it is rebuilt by the deploy.
