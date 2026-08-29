# syntax=docker/dockerfile:1.7
FROM node:24.19.0-alpine AS build

ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
WORKDIR /src

RUN corepack enable && corepack prepare pnpm@11.21.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY web ./web
RUN pnpm install --frozen-lockfile
ARG VITE_API_MODE=real
ARG VITE_API_BASE_URL=/
# Card origin and platform login URL are resolved at runtime from
# /argus-runtime.json (injected by Helm); VITE_* values are dev/mock overrides.
ARG VITE_CARD_ORIGIN=
ARG VITE_PLATFORM_URL=
ARG VITE_DIRECT_EGRESS_ADDRESSES=
RUN VITE_API_MODE=$VITE_API_MODE \
    VITE_API_BASE_URL=$VITE_API_BASE_URL \
    VITE_CARD_ORIGIN=$VITE_CARD_ORIGIN \
    VITE_PLATFORM_URL=$VITE_PLATFORM_URL \
    VITE_DIRECT_EGRESS_ADDRESSES=$VITE_DIRECT_EGRESS_ADDRESSES \
    pnpm --filter @argus/platform build && \
    pnpm --filter @argus/enterprise build && \
    pnpm --filter @argus/card-runtime build

FROM nginxinc/nginx-unprivileged:1.29.4-alpine
COPY deploy/docker/nginx.conf /etc/nginx/nginx.conf
COPY --from=build /src/web/apps/enterprise/dist /srv/enterprise
COPY --from=build /src/web/apps/platform/dist /srv/platform
COPY --from=build /src/web/apps/card-runtime/dist /srv/card-runtime
EXPOSE 8080 8081 8083
