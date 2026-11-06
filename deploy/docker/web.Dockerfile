# Builds one frontend app (APP=client or APP=admin) and serves it as static files.
FROM node:26-alpine AS build
RUN npm install -g pnpm@12.18.0
WORKDIR /src
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/ apps/
COPY packages/ packages/
ARG APP
RUN pnpm install --frozen-lockfile --filter "@msp/${APP}..."
RUN pnpm --filter "@msp/${APP}" build && cp -r "apps/${APP}/dist" /out

FROM nginxinc/nginx-unprivileged:1.30-alpine
COPY deploy/docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /out /usr/share/nginx/html
