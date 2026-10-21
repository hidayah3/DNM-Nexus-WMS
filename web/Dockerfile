FROM node:22-alpine AS build
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY index.html ./
COPY src ./src
ARG VITE_API_BASE_URL=
ARG VITE_WMS_API_ENABLED=false
ARG VITE_WMS_COURIERS=[]
ARG VITE_WMS_READ_ONLY_PREVIEW=false
ARG VITE_WMS_EASYPARCEL_CONNECT=false
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
ENV VITE_WMS_API_ENABLED=${VITE_WMS_API_ENABLED}
ENV VITE_WMS_COURIERS=${VITE_WMS_COURIERS}
ENV VITE_WMS_READ_ONLY_PREVIEW=${VITE_WMS_READ_ONLY_PREVIEW}
ENV VITE_WMS_EASYPARCEL_CONNECT=${VITE_WMS_EASYPARCEL_CONNECT}
RUN pnpm build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
