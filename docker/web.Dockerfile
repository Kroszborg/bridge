# syntax=docker/dockerfile:1
# Bridge website: a static Next.js export served by nginx.

FROM node:24-alpine AS base
ENV PNPM_HOME=/pnpm COREPACK_ENABLE_DOWNLOAD_PROMPT=0 NEXT_TELEMETRY_DISABLED=1
ENV PATH=$PNPM_HOME:$PATH
# Node 25+ no longer ships corepack, so install it explicitly; it then provides the
# pnpm version pinned in package.json (packageManager).
RUN npm install -g corepack@0.36.0 && corepack enable
WORKDIR /repo

FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --frozen-lockfile --filter "@bridge/web..."

FROM deps AS build
# Baked into the static pages at build time.
ARG NEXT_PUBLIC_DASHBOARD_URL=http://localhost:3000
ARG NEXT_PUBLIC_SITE_URL=http://localhost:3001
ENV NEXT_PUBLIC_DASHBOARD_URL=$NEXT_PUBLIC_DASHBOARD_URL NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL
# The /docs pages and llms-full.txt are built from the repository's Markdown.
COPY README.md ./
COPY docs docs
COPY packages/sdk/README.md packages/sdk/
COPY apps/web apps/web
RUN pnpm --filter @bridge/web build

FROM nginx:1.29-alpine AS runtime
COPY docker/web.nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/apps/web/out /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
