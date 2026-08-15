# syntax=docker/dockerfile:1.7
FROM node:24.19.0-alpine AS build

ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
WORKDIR /src

RUN corepack enable && corepack prepare pnpm@11.21.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig.base.json ./
COPY web ./web
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @argus/setup build && \
    pnpm --filter @argus/platform build && \
    pnpm --filter @argus/enterprise build

FROM nginxinc/nginx-unprivileged:1.29.4-alpine
COPY deploy/docker/nginx.conf /etc/nginx/nginx.conf
COPY --from=build /src/web/apps/enterprise/dist /srv/enterprise
COPY --from=build /src/web/apps/platform/dist /srv/platform
COPY --from=build /src/web/apps/setup/dist /srv/setup
EXPOSE 8080 8081 8082
