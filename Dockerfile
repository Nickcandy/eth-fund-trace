FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/eth-fund-trace ./cmd/server

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=server /out/eth-fund-trace ./eth-fund-trace
COPY --from=web /src/web/dist ./web/dist
USER app
EXPOSE 8080
ENTRYPOINT ["/app/eth-fund-trace"]
