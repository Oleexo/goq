FROM node:20-alpine AS base
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM base AS dev
COPY . .
EXPOSE 3000
CMD ["npm", "run", "start", "--", "--host", "0.0.0.0", "--poll", "1000"]

FROM base AS build
COPY . .
RUN npm run build

FROM node:20-alpine AS prod
WORKDIR /app
COPY --from=build /app/build ./build
COPY --from=base /app/node_modules ./node_modules
COPY --from=build /app/package.json ./package.json
EXPOSE 3000
CMD ["npm", "run", "serve", "--", "--host", "0.0.0.0", "--port", "3000", "--no-open"]
