# ================= Stage 1: Frontend Builder (构建前端) =================
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend

# Alpine 换源加速
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories

# 安装构建原生模块所需的依赖
RUN apk add --no-cache python3 make g++

# npm 换源加速
RUN npm config set registry https://registry.npmmirror.com

# 安装依赖
COPY frontend/package*.json ./
RUN npm install

# 编译生成 dist 文件夹
COPY frontend/ ./
RUN npm run build


# ================= Stage 2: Backend Builder (构建后端) =================
FROM golang:1.23-alpine AS backend-builder
WORKDIR /app/backend

# Alpine 换源加速
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories
RUN apk add --no-cache git build-base

# Go 代理加速
ENV GOPROXY=https://goproxy.cn,direct

# 下载后端依赖
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# 拷贝后端源码
COPY backend/ ./

# --- 关键点 1: 将前端产物拷贝进来，准备打包 ---
# 确保拷贝到 backend 目录下的 static 文件夹
COPY --from=frontend-builder /app/frontend/dist ./static

# 编译 Go 二进制文件
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server main.go


# ================= Stage 3: Final Runtime (最终运行镜像) =================
FROM alpine:3.20
WORKDIR /app

# Alpine 换源并安装运行时必要的包
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories && \
    apk add --no-cache ca-certificates tzdata && \
    update-ca-certificates

# 创建非 root 用户
RUN addgroup -S app && adduser -S app -G app

# --- 关键点 2: 将所有产物拷贝到最终镜像 ---
# 拷贝后端可执行程序
COPY --from=backend-builder /app/server /app/server
# 拷贝前端静态资源 (解决 404 的核心)
COPY --from=backend-builder /app/backend/static /app/static

# --- 关键点 3: 递归修改权限，确保 app 用户能读取 static 目录 ---
RUN chown -R app:app /app

USER app
EXPOSE 8080
ENV APP_PORT=8080

# 启动
CMD ["/app/server"]