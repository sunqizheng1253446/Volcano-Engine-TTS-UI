# 字节跳动火山引擎TTS v3 API 转 OpenAI 兼容接口

## 项目简介

本项目将字节跳动火山引擎TTS（文本转语音）v3 API封装为OpenAI兼容的TTS API接口，使原本调用OpenAI TTS服务的应用可以无缝切换到火山引擎TTS服务。

### 主要特性

- ✅ 完全兼容OpenAI `/v1/audio/speech` API接口
- ✅ 支持火山引擎TTS v3 API（单向流式）
- ✅ 支持两种鉴权模式：Legacy(AppID+AccessKey) 和 New(API Key)
- ✅ 支持多种发音人和模型版本
- ✅ 内置速率限制和统计功能
- ✅ 支持配置API密钥验证
- ✅ 跨平台支持（Windows/Linux/macOS）

## 文件说明

- `tts_server.go` - 主程序源码
- `.env.example` - 环境变量配置示例
- `go.mod` / `go.sum` - Go模块依赖

## 快速开始

### 前置要求

- Go 1.19 或更高版本
- 火山引擎账号并开通TTS服务

### 1. 编译程序

```bash
go build -o tts_server tts_server.go
```

### 2. 配置环境变量

复制 `.env.example` 为 `.env` 并填入你的配置：

```bash
cp .env.example .env
```

编辑 `.env` 文件，填入必要的配置参数。

### 3. 启动服务

```bash
# Windows
tts_server.exe

# Linux/macOS
./tts_server
```

服务默认监听 `8080` 端口。

## 环境变量配置

### 必需参数

| 变量名 | 说明 | 示例 |
|--------|------|------|
| `BYTEDANCE_TTS_AUTH_MODE` | 鉴权模式：`legacy` 或 `new` | `legacy` |
| `BYTEDANCE_TTS_RESOURCE_ID` | 资源ID，决定模型版本 | `seed-tts-1.0` |
| `BYTEDANCE_TTS_SPEAKER` | 发音人（音色）ID | `zh_female_qingxin` |

### 鉴权参数（二选一）

**Legacy 模式（旧版控制台）：**
- `BYTEDANCE_TTS_APP_ID` - 应用ID
- `BYTEDANCE_TTS_ACCESS_KEY` - 访问密钥

**New 模式（新版控制台）：**
- `BYTEDANCE_TTS_API_KEY` - API密钥

### 可选参数

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| `BYTEDANCE_TTS_TIMEOUT` | 请求超时时间 | `30s` |
| `OPENAI_TTS_API_KEY` | OpenAI兼容接口的API密钥（逗号分隔支持多个） | 无 |
| `PORT` | 服务监听端口 | `8080` |

### Resource ID 说明

| Resource ID | 模型说明 |
|-------------|----------|
| `seed-tts-1.0` | 豆包语音合成模型1.0字符版 |
| `seed-tts-1.0-concurr` | 豆包语音合成模型1.0并发版 |
| `seed-tts-2.0` | 豆包语音合成模型2.0字符版 |
| `seed-icl-1.0` | 声音复刻1.0字符版 |
| `seed-icl-1.0-concurr` | 声音复刻1.0并发版 |
| `seed-icl-2.0` | 声音复刻2.0字符版 |

**注意：** 1.0音色只能搭配 `seed-tts-1.0` Resource ID，2.0音色只能搭配 `seed-tts-2.0` Resource ID。

## API 使用说明

### OpenAI 兼容接口

**端点：** `POST /v1/audio/speech`

**请求头：**
- `Content-Type: application/json`
- `Authorization: Bearer <你的API密钥>`（如果配置了OPENAI_TTS_API_KEY）

**请求体：**
```json
{
  "model": "tts-1",
  "input": "你好，这是一个测试文本",
  "voice": "alloy",
  "response_format": "wav",
  "speed": 1.0
}
```

**参数说明：**
- `model` - 模型名称（OpenAI兼容，实际不影响）
- `input` - 要合成的文本
- `voice` - 发音人（OpenAI兼容，实际不影响）
- `response_format` - 输出格式：`wav`、`mp3`、`opus`、`aac`、`flac`
- `speed` - 语速：0.25 ~ 4.0

**示例调用：**

```bash
curl -X POST "http://localhost:8080/v1/audio/speech" \
  -H "Content-Type: application/json" \
  -d '{"model":"tts-1","input":"你好，世界","voice":"alloy","speed":1.0}' \
  -o output.wav
```

### 健康检查

```bash
curl http://localhost:8080/health
```

### 统计信息

```bash
curl http://localhost:8080/stats
```

## 支持的发音人

具体发音人列表请参考火山引擎官方文档：
- 1.0音色：https://www.volcengine.com/docs/6561/97454
- 2.0音色：https://www.volcengine.com/docs/6561/1340515

## 常见问题

### 1. 如何获取鉴权信息？

- 登录火山引擎控制台
- 进入"语音合成"服务
- 在应用管理中创建应用并获取AppID和AccessKey（Legacy模式）
- 或在新版控制台获取API Key（New模式）

### 2. 端口被占用怎么办？

通过环境变量修改端口：

```bash
# Windows
set PORT=8081 && tts_server.exe

# Linux/macOS
PORT=8081 ./tts_server
```

### 3. 如何配置多个API密钥？

使用逗号分隔：

```bash
OPENAI_TTS_API_KEY=sk-key1,sk-key2,sk-key3
```

### 4. 查看日志

服务启动后会输出详细日志，包括：
- 服务启动信息
- 鉴权模式和配置状态
- 请求统计信息
- 错误详情

## 部署建议

### Linux Systemd 服务

创建 `/etc/systemd/system/tts-server.service`：

```ini
[Unit]
Description=ByteDance TTS to OpenAI API Adapter
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/www/wwwroot/tts-server
EnvironmentFile=/www/wwwroot/tts-server/.env
ExecStart=/www/wwwroot/tts-server/tts_server
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

启动服务：

```bash
sudo systemctl daemon-reload
sudo systemctl enable tts-server
sudo systemctl start tts-server
```

## 许可证

本项目采用非商业用途许可协议。您可以免费使用本软件用于非商业目的，但禁止用于任何商业活动。详细条款请参阅 [LICENSE](LICENSE) 文件。

## 技术支持

如有问题，请检查：
1. 环境变量配置是否正确
2. 网络是否能访问火山引擎TTS服务
3. 鉴权信息是否有效
4. Resource ID与Speaker是否匹配
