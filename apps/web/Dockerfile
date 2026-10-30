FROM node:24-alpine AS build
WORKDIR /app
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY apps/web/ ./
ENV NEXT_TELEMETRY_DISABLED=1
ARG NEXT_PUBLIC_API_PUBLIC_URL=http://localhost:8080
ENV NEXT_PUBLIC_API_PUBLIC_URL=$NEXT_PUBLIC_API_PUBLIC_URL
RUN npm run build

FROM node:24-alpine
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=build /app/package.json ./
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/.next ./.next
COPY --from=build /app/next.config.ts ./next.config.ts
USER node
EXPOSE 3000
CMD ["npm", "run", "start"]
