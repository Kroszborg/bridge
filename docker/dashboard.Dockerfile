# syntax=docker/dockerfile:1
# Bridge dashboard: Next.js standalone server on Node 24 LTS.

FROM node:25-alpine AS base
ENV PNPM_HOME=/pnpm COREPACK_ENABLE_DOWNLOAD_PROMPT=0 NEXT_TELEMETRY_DISABLED=1
ENV PATH=$PNPM_HOME:$PATH
# Node 25+ no longer ships corepack, so install it explicitly; it then provides the
# pnpm version pinned in package.json (packageManager).
RUN npm install -g corepack@0.36.0 && corepack enable
WORKDIR /repo

FROM base AS deps
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/dashboard/package.json apps/dashboard/
COPY packages/api-types/package.json packages/api-types/
COPY tools/openapi-codegen/package.json tools/openapi-codegen/
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --frozen-lockfile --filter "@bridge/dashboard..."

FROM deps AS build
COPY packages/api-types packages/api-types
COPY apps/dashboard apps/dashboard
RUN pnpm --filter @bridge/dashboard build

FROM node:25-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0
RUN addgroup -S bridge && adduser -S -G bridge bridge
COPY --from=build --chown=bridge:bridge /repo/apps/dashboard/.next/standalone ./
COPY --from=build --chown=bridge:bridge /repo/apps/dashboard/.next/static ./apps/dashboard/.next/static
COPY --from=build --chown=bridge:bridge /repo/apps/dashboard/public ./apps/dashboard/public
USER bridge
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=3s --retries=5 \
  CMD wget -qO- http://127.0.0.1:3000/login >/dev/null || exit 1
CMD ["node", "apps/dashboard/server.js"]
