#!/bin/bash

# Docker 部署脚本
# 使用方法: ./docker-deploy.sh [dev|prod]

set -e

ENV=${1:-dev}

echo "=========================================="
echo "数据库同步系统 Docker 部署脚本"
echo "环境: $ENV"
echo "=========================================="

# 检查 Docker 是否安装
if ! command -v docker &> /dev/null; then
    echo "错误: 未安装 Docker，请先安装 Docker"
    exit 1
fi

# 检查 Docker Compose 是否安装
if ! command -v docker-compose &> /dev/null; then
    echo "错误: 未安装 Docker Compose，请先安装 Docker Compose"
    exit 1
fi

# 检查配置文件
if [ ! -f "config.json" ]; then
    echo "警告: 未找到 config.json 配置文件"
    echo "正在从 config.json.example 创建..."
    if [ -f "config.json.example" ]; then
        cp config.json.example config.json
        echo "请编辑 config.json 文件，配置数据库连接等信息"
        read -p "按 Enter 继续，或 Ctrl+C 退出..."
    else
        echo "错误: 未找到 config.json.example 文件"
        exit 1
    fi
fi

# 创建日志目录
mkdir -p logs
chmod 777 logs

# 根据环境选择不同的 compose 文件
if [ "$ENV" = "prod" ]; then
    echo "使用生产环境配置..."
    COMPOSE_FILE="docker-compose.prod.yml"
    
    # 检查生产环境配置文件
    if [ ! -f "config.prod.json" ]; then
        echo "警告: 未找到 config.prod.json 配置文件"
        echo "正在从 config.json.example 创建..."
        cp config.json.example config.prod.json
        echo "请编辑 config.prod.json 文件，配置生产环境信息"
        read -p "按 Enter 继续，或 Ctrl+C 退出..."
    fi
else
    echo "使用开发环境配置..."
    COMPOSE_FILE="docker-compose.yml"
fi

# 构建镜像
echo ""
echo "正在构建 Docker 镜像..."
docker-compose -f $COMPOSE_FILE build

# 启动服务
echo ""
echo "正在启动服务..."
docker-compose -f $COMPOSE_FILE up -d

# 等待服务启动
echo ""
echo "等待服务启动..."
sleep 5

# 检查服务状态
echo ""
echo "服务状态:"
docker-compose -f $COMPOSE_FILE ps

# 显示日志
echo ""
echo "=========================================="
echo "服务已启动！"
echo "=========================================="
echo ""
echo "查看日志: docker-compose -f $COMPOSE_FILE logs -f db-sync"
echo "停止服务: docker-compose -f $COMPOSE_FILE down"
echo "重启服务: docker-compose -f $COMPOSE_FILE restart db-sync"
echo ""
echo "API 地址: http://localhost:8080"
echo "健康检查: http://localhost:8080/api/v1/health"
echo ""

