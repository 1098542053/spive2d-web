# Stage 1: 编译 Go 后端
FROM golang:1.22-alpine AS backend
WORKDIR /app
COPY server-go/ .

ARG GIT_SHA=dev
ARG BUILD_TIME=unknown
RUN go build -ldflags="-X main.buildVersion=${GIT_SHA} -X main.buildTime=${BUILD_TIME}" -o server .

# Stage 2: 运行阶段
FROM alpine:3.19

# 复制 Go 编译产物
COPY --from=backend /app/server /server

# 复制前端构建产物（包括 lib/ 下的 Live2D/Spine 运行时）
COPY build/ /app/build/

# 环境变量
ENV MODELS_ROOT=/data/models
ENV PORT=3000

EXPOSE 3000
CMD ["/server"]
