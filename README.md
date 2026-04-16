# 群面系统（Vue + Go + WebRTC）

这是一个可部署的群面演示系统，满足以下核心需求：

- 顶部导航文案按示例页面保留：`智聘AI`、`学生端`、`首页`、`简历解析`、`刷题`、`模拟面试`、`复盘报告`、`校友社区`
- 群面房间固定 5 个面试者窗口（无真人面试官）
- 单独出题气泡窗口，可开始后切换题目
- 每位面试者可单独开关麦克风，不提供视频开关（视频固定开启）
- 公屏聊天
- 登录、注册、邀请码创建、邀请码加入、房间列表、进入房间
- 至少 3 位面试者加入后才可开始群面

## 目录结构

- `frontend/` Vue 3 + Vite 前端
- `backend/` Go 服务（REST + WebSocket 信令 + 静态资源托管）
- `deploy/coturn/` TURN 配置模板
- `Dockerfile` 多阶段构建（前端打包 + 后端编译）
- `docker-compose.yml` 一键部署 `app + coturn`

## 本地开发

### 1) 启动后端

```bash
cd backend
go mod tidy
go run main.go
```

后端默认监听 `8080`。

### 2) 启动前端开发服务

```bash
cd frontend
npm install
npm run dev
```

前端默认 `5173`，已配置代理到 `8080`。

## 一体化运行（后端直出前端页面）

你已经可以使用当前仓库中的 `backend/static` 直接运行 Go 服务并访问页面。

如果你改动了前端代码，需要重新构建并覆盖静态资源：

```bash
cd frontend
npm run build
cd ..
rm -rf backend/static
cp -r frontend/dist backend/static
```

## Docker 部署（推荐）

```bash
docker compose up --build -d
```

默认访问：`http://<服务器IP>:8080`

### TURN 关键配置（重要）

为了提高跨网络环境下的音视频连通率，请修改：

- `deploy/coturn/turnserver.conf` 中 `external-ip=YOUR_SERVER_PUBLIC_IP`
- `docker-compose.yml` 中 `WEBRTC_TURN_URL` 里的 `YOUR_SERVER_PUBLIC_IP`

并确保云服务器安全组放行：

- `8080/tcp`（应用）
- `3478/tcp + 3478/udp`（TURN）
- `49160-49200/udp`（TURN 中继端口）

## 后端接口概览

- `POST /api/auth/register` 注册
- `POST /api/auth/login` 登录
- `GET /api/auth/me` 当前用户
- `POST /api/invites` 创建群面邀请码
- `GET /api/invites/:code` 查看邀请码对应房间
- `POST /api/invites/:code/accept` 通过邀请码加入
- `GET /api/rooms/mine` 我的房间
- `GET /api/rooms/:roomId/state` 房间状态
- `POST /api/rooms/:roomId/start` 开始群面（最少 3 人）
- `POST /api/rooms/:roomId/question/next` 切换题目
- `GET /api/config/webrtc` ICE 配置
- `GET /ws?roomId=...&token=...` WebSocket 信令/公屏/房间状态

## 说明

- 当前实现采用内存存储（重启后账号和房间数据会清空）。
- WebRTC 采用 Mesh 方案，5 人规模可用。
- 真正的“绝对保证可接通”在公网环境依赖 TURN 与网络条件，已提供 TURN 配置模板与容器。实际部署时请按你的公网 IP/域名改配置。
