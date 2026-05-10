#!/bin/bash

# ByteDance TTS to OpenAI API Adapter 启动脚本 (Linux优化版)
# 使用Unix LF换行符，避免Windows CRLF问题

# 设置Go代理（中国大陆用户推荐）
export GOPROXY=https://goproxy.cn,direct

# 项目目录 - 自动获取当前目录
PROJECT_DIR=$(pwd)
BINARY_NAME="tts-server"
MAIN_FILE="tts_server.go"
PID_FILE="$PROJECT_DIR/tts-server.pid"
LOG_FILE="$PROJECT_DIR/logs/tts-server.log"

# 创建日志目录
mkdir -p "$PROJECT_DIR/logs"

# 检查Go环境
check_go() {
    if ! command -v go &> /dev/null; then
        echo "❌ Go 未安装或未添加到 PATH"
        echo "请先安装 Go 语言环境"
        return 1
    fi
    echo "✅ Go 环境检查通过: $(go version)"
    return 0
}

# 检查依赖
check_deps() {
    echo "正在检查 Go 模块依赖..."
    cd $PROJECT_DIR
    if [ ! -f go.mod ]; then
        echo "❌ 未找到 go.mod 文件"
        return 1
    fi
    
    # 下载依赖
    go mod download
    if [ $? -ne 0 ]; then
        echo "❌ Go 依赖下载失败"
        return 1
    fi
    
    echo "✅ Go 依赖检查完成"
    return 0
}

# 函数：启动服务
start() {
    echo "🚀 正在启动 TTS API 服务器..."
    
    # 检查Go环境
    if ! check_go; then
        return 1
    fi
    
    # 检查是否已经运行
    if [ -f $PID_FILE ]; then
        PID=$(cat $PID_FILE)
        if ps -p $PID > /dev/null 2>&1; then
            echo "⚠️  服务已经在运行中 (PID: $PID)"
            return 1
        fi
    fi
    
    # 切换到项目目录
    cd $PROJECT_DIR
    
    # 检查主程序文件
    if [ ! -f $MAIN_FILE ]; then
        echo "❌ 未找到主程序文件: $MAIN_FILE"
        echo "请确保 $MAIN_FILE 文件存在"
        return 1
    fi
    
    # 检查依赖
    if ! check_deps; then
        return 1
    fi
    
    # 构建项目
    echo "🔨 正在构建项目..."
    go build -o $BINARY_NAME $MAIN_FILE
    
    if [ $? -ne 0 ]; then
        echo "❌ 构建失败"
        return 1
    fi
    
    echo "✅ 构建成功"
    
    # 启动服务
    echo "🌟 正在启动服务..."
    nohup ./$BINARY_NAME > $LOG_FILE 2>&1 &
    PID=$!
    echo $PID > $PID_FILE
    
    # 等待启动
    sleep 2
    
    # 检查启动状态
    if ps -p $PID > /dev/null 2>&1; then
        echo "✅ TTS API 服务器启动成功 (PID: $PID)"
        echo "📍 端口: 8080"
        echo "📄 日志文件: $LOG_FILE"
        echo "🌐 访问地址: http://你的服务器IP:8080"
        echo "💚 健康检查: http://你的服务器IP:8080/health"
        return 0
    else
        echo "❌ 服务启动失败，请检查日志: $LOG_FILE"
        rm -f $PID_FILE
        return 1
    fi
}

# 函数：停止服务
stop() {
    echo "🛑 正在停止 TTS API 服务器..."
    
    if [ -f $PID_FILE ]; then
        PID=$(cat $PID_FILE)
        if ps -p $PID > /dev/null 2>&1; then
            # 优雅关闭（发送 SIGTERM）
            kill -TERM $PID
            
            # 等待进程结束
            for i in {1..10}; do
                if ! ps -p $PID > /dev/null 2>&1; then
                    echo "✅ 服务已优雅停止 (PID: $PID)"
                    rm -f $PID_FILE
                    return 0
                fi
                sleep 1
            done
            
            # 强制杀死进程
            echo "⚠️  进程未响应，强制终止..."
            kill -KILL $PID
            echo "✅ 服务已强制停止 (PID: $PID)"
            rm -f $PID_FILE
        else
            echo "⚠️  服务未运行"
            rm -f $PID_FILE
        fi
    else
        echo "⚠️  PID文件不存在，服务可能未运行"
    fi
}

# 函数：重启服务
restart() {
    echo "🔄 正在重启 TTS API 服务器..."
    stop
    sleep 2
    start
}

# 函数：查看状态
status() {
    echo "📊 TTS API 服务器状态："
    echo "================================"
    
    if [ -f $PID_FILE ]; then
        PID=$(cat $PID_FILE)
        if ps -p $PID > /dev/null 2>&1; then
            echo "🟢 状态: 运行中"
            echo "🆔 PID: $PID"
            echo "🕐 运行时间: $(ps -o etime= -p $PID | tr -d ' ')"
            echo "💾 内存使用: $(ps -o rss= -p $PID | tr -d ' ') KB"
            echo "🌐 端口: 8080"
            echo "📄 日志: $LOG_FILE"
            
            # 检查端口是否监听
            if command -v netstat &> /dev/null; then
                if netstat -tlnp 2>/dev/null | grep ":8080" | grep "$PID" > /dev/null; then
                    echo "🔗 端口监听: ✅"
                else
                    echo "🔗 端口监听: ❌"
                fi
            fi
        else
            echo "🔴 状态: 未运行（PID文件存在但进程不存在）"
            rm -f $PID_FILE
        fi
    else
        echo "🔴 状态: 未运行"
    fi
    
    echo "================================"
}

# 函数：查看日志
logs() {
    if [ -f $LOG_FILE ]; then
        echo "📄 实时日志 (按 Ctrl+C 退出):"
        echo "================================"
        tail -f $LOG_FILE
    else
        echo "❌ 日志文件不存在: $LOG_FILE"
    fi
}

# 函数：查看最近日志
lastlog() {
    if [ -f $LOG_FILE ]; then
        echo "📄 最近 50 行日志:"
        echo "================================"
        tail -n 50 $LOG_FILE
    else
        echo "❌ 日志文件不存在: $LOG_FILE"
    fi
}

# 函数：测试服务
test() {
    echo "🧪 正在测试 TTS API 服务器..."
    
    # 检查健康状态
    echo "1. 健康检查测试..."
    if command -v curl &> /dev/null; then
        response=$(curl -s -w "%{http_code}" -o /tmp/health_check.tmp http://localhost:8080/health)
        if [ "$response" = "200" ]; then
            echo "✅ 健康检查通过"
            cat /tmp/health_check.tmp | python3 -m json.tool 2>/dev/null || cat /tmp/health_check.tmp
        else
            echo "❌ 健康检查失败 (HTTP: $response)"
        fi
        rm -f /tmp/health_check.tmp
    else
        echo "⚠️  curl 未安装，跳过健康检查"
    fi
}

# 显示帮助信息
usage() {
    echo "ByteDance TTS to OpenAI API Adapter 管理脚本 (Linux优化版)"
    echo ""
    echo "使用方法: $0 {命令}"
    echo ""
    echo "可用命令："
    echo "  start     - 启动服务"
    echo "  stop      - 停止服务"  
    echo "  restart   - 重启服务"
    echo "  status    - 查看详细状态"
    echo "  logs      - 实时查看日志"
    echo "  lastlog   - 查看最近日志"
    echo "  test      - 测试服务"
    echo "  help      - 显示帮助信息"
    echo ""
    echo "示例："
    echo "  ./start_linux.sh start"
    echo "  ./start_linux.sh status"
    echo "  ./start_linux.sh logs"
}

# 主程序逻辑
case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        restart
        ;;
    status)
        status
        ;;
    logs)
        logs
        ;;
    lastlog)
        lastlog
        ;;
    test)
        test
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        echo "❌ 未知命令: $1"
        echo ""
        usage
        exit 1
        ;;
esac

exit $?
