# Build stage
FROM oven/bun:1-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json* bun.lock* ./
RUN bun install

ARG CACHEBUST=1
COPY . .
RUN bun run build

# Production runtime stage using Bun
FROM oven/bun:1-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV PORT=3000

COPY --from=builder /app/dist ./dist
COPY --from=builder /app/server.ts ./server.ts
COPY --from=builder /app/package.json ./package.json

EXPOSE 3000

CMD ["bun", "run", "server.ts"]
