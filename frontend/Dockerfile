# Imagen del frontend Next.js (salida standalone). Multi-stage: instalar deps →
# compilar → servir con un runtime mínimo no-root.

FROM node:20-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci

FROM node:20-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
# OJO: NEXT_PUBLIC_* se INCRUSTA en el bundle en tiempo de BUILD, no de runtime.
# Es la URL que el NAVEGADOR (en el host) usa para llamar a la API, así que por
# defecto apunta al puerto publicado en el host (localhost:8080), no a "api:8080"
# (que el navegador no sabría resolver).
ARG NEXT_PUBLIC_API_URL=http://localhost:8080
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
RUN npm run build

FROM node:20-alpine AS run
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
RUN addgroup -S nodejs && adduser -S nextjs -G nodejs
# La salida standalone trae su propio server.js y el node_modules mínimo.
COPY --from=build /app/public ./public
COPY --from=build --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=build --chown=nextjs:nodejs /app/.next/static ./.next/static
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
