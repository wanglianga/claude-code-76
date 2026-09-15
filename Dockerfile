# ---------- 构建阶段 ----------
FROM golang:1.23-alpine AS build
WORKDIR /app
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server .

# ---------- 运行阶段 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 appuser
WORKDIR /app
COPY --from=build /app/server .
USER appuser
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
CMD ["./server"]
