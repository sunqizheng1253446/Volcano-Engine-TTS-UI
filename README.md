# ByteDance TTS to OpenAI API Adapter - Linux部署指南

## 快速开始

这是为Linux环境优化的TTS服务器版本，可以将字节跳动TTS服务适配为OpenAI TTS API格式。

## 文件说明

- `tts_server_linux.go` - Linux优化版主程序
- `start_linux.sh` - Linux启动脚本
- `fix_crlf.sh` - 换行符修复工具
- `go.mod` 和 `go.sum` - Go模块依赖文件

## 部署步骤

### 1. 上传文件到Linux服务器

将本文件夹中的所有文件上传到Linux服务器，建议放在`/www/wwwroot/tts-server/`目录。

### 2. 修复换行符（重要！）

在Linux服务器上，执行以下命令修复可能存在的Windows CRLF换行符问题：

```bash
cd /www/wwwroot/tts-server
chmod +x fix_crlf.sh
./fix_crlf.sh
```

### 3. 安装Go环境（如果未安装）

```bash
# 下载Go 1.21.5
cd /tmp
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz

# 解压到/usr/local
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz

# 设置环境变量
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export GOPROXY=https://goproxy.cn,direct' >> ~/.bashrc
source ~/.bashrc

# 验证安装
go version
```

### 4. 启动服务

```bash
cd /www/wwwroot/tts-server
chmod +x start_linux.sh
./start_linux.sh start
```

## 服务管理命令

- 启动服务：`./start_linux.sh start`
- 停止服务：`./start_linux.sh stop`
- 重启服务：`./start_linux.sh restart`
- 查看状态：`./start_linux.sh status`
- 查看日志：`./start_linux.sh logs`

## 接口使用

- OpenAI TTS API兼容端点：`http://你的服务器IP:8080/v1/audio/speech`
- 健康检查：`http://你的服务器IP:8080/health`

## API调用示例

```bash
curl -X POST "http://你的服务器IP:8080/v1/audio/speech" \
  -H "Authorization: Bearer sk-7IBXpzK1YszwGArEMvLGzSdZe93rXVxg4CFBe5KRlqs4dVJO" \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","input":"你好，这是一个测试文本","voice":"alloy","speed":1.0}' \
  -o output.wav
```

## 常见问题

### 端口被占用

如果8080端口被占用，可以通过环境变量修改端口：

```bash
export PORT=8081 && ./start_linux.sh start
```

### 启动失败

查看日志文件获取详细信息：

```bash
cat /www/wwwroot/tts-server/logs/tts-server.log
```

## 环境变量配置

服务现在支持通过环境变量配置所有参数，推荐使用这种方式而不是直接修改代码。

### 必须的环境变量

- `BYTEDANCE_TTS_APP_ID` - 字节跳动TTS应用ID
- `BYTEDANCE_TTS_BEARER_TOKEN` - 字节跳动TTS访问令牌
- `BYTEDANCE_TTS_CLUSTER` - 字节跳动TTS业务集群
- `BYTEDANCE_TTS_VOICE_TYPE` - 字节跳动TTS声音类型

### 可选的环境变量

- `BYTEDANCE_TTS_ENDPOINT` - 字节跳动TTS服务端点（默认：https://openspeech.bytedance.com/api/v1/tts）
- `BYTEDANCE_TTS_TIMEOUT` - 字节跳动TTS请求超时时间（默认：30s，支持Go duration格式，如"60s"、"1m"）
- `OPENAI_TTS_API_KEY` - OpenAI TTS API密钥（未设置时允许任意key）
- `PORT` - 服务监听端口（默认：8080）

### 环境变量使用示例

```bash
# 启动服务时设置环境变量
export BYTEDANCE_TTS_APP_ID=your_app_id
export BYTEDANCE_TTS_BEARER_TOKEN=your_token
export BYTEDANCE_TTS_CLUSTER=your_cluster
export BYTEDANCE_TTS_VOICE_TYPE=your_voice_type
export OPENAI_TTS_API_KEY=sk-your_api_key
export PORT=8081
./start_linux.sh start
```

## 注意事项

1. 确保Go版本至少为1.19
2. 确保服务器有足够的网络权限访问字节跳动TTS服务
3. 必须设置所有必需的环境变量，否则服务会使用默认值并显示警告信息
4. 可以通过设置环境变量而不是直接修改代码来配置服务

## 许可证

本项目采用非商业用途许可协议。您可以免费使用本软件用于非商业目的，但禁止用于任何商业活动。详细条款请参阅项目中的 [LICENSE](LICENSE) 文件。