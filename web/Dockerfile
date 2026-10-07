# Build stage
FROM node:22-alpine AS builder

WORKDIR /app

# Install dependencies
COPY package.json package-lock.json* bun.lock* ./
RUN npm install

# Build static assets
ARG CACHEBUST=1
COPY . .
RUN npm run build

# Production runtime stage
FROM nginx:alpine
ARG CACHEBUST=1

# Copy nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy compiled static assets from builder stage
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
