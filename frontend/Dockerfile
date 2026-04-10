# ─── Stage 1: Build ───────────────────────────────────────────────────────────
FROM node:20-alpine AS builder

WORKDIR /app

# Copy manifests first — npm ci only reruns when these files change.
COPY package.json package-lock.json ./
RUN npm ci

# Copy the rest of the source.
COPY . .

# VITE_API_BASE_URL is intentionally left empty here.
# The API URL strategy (build-time vs runtime injection) is being finalised
# separately. For now, the Vite dev proxy handles local development and the
# value can be supplied as a build-arg for manual builds:
#   docker build --build-arg VITE_API_BASE_URL=http://my-backend .
ARG VITE_API_BASE_URL=""
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL

RUN npm run build

# ─── Stage 2: Serve ───────────────────────────────────────────────────────────
# nginx:alpine is ~40 MB. The node_modules and all build tooling from Stage 1
# are discarded — only the compiled /dist files are copied across.
FROM nginx:alpine

# Replace the default nginx config with our SPA-aware one.
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy the compiled React app from the build stage.
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
