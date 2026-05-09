package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// TTSServResponse response from backend srvs
type TTSServResponse struct {
	ReqID     string `json:"reqid"`
	Code      int    `json:"code"`
	Message   string `json:"Message"`
	Operation string `json:"operation"`
	Sequence  int    `json:"sequence"`
	Data      string `json:"data"`
}

// OpenAI TTS API请求格式
type OpenAITTSRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

// 字节跳动TTS配置
type ByteDanceTTSConfig struct {
	AppID       string
	BearerToken string
	Cluster     string
	URL         string
	VoiceType   string
	Timeout     time.Duration
}

// API密钥配置
var VALID_API_KEY string

// 全局配置
var ttsConfig ByteDanceTTSConfig

// 初始化字节跳动TTS配置
func initTTSConfig() {
	missingVars := []string{}
	
	// 读取必须的环境变量
	appID := os.Getenv("BYTEDANCE_TTS_APP_ID")
	if appID == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_APP_ID")
	}
	
bearerToken := os.Getenv("BYTEDANCE_TTS_BEARER_TOKEN")
	if bearerToken == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_BEARER_TOKEN")
	}
	
	cluster := os.Getenv("BYTEDANCE_TTS_CLUSTER")
	if cluster == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_CLUSTER")
	}
	
	voiceType := os.Getenv("BYTEDANCE_TTS_VOICE_TYPE")
	if voiceType == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_VOICE_TYPE")
	}
	
	// 如果有缺失的必须变量，输出错误信息并使用默认值继续运行（但可能会导致功能失败）
	if len(missingVars) > 0 {
		log.Printf("警告: 缺少以下必须的环境变量: %v", missingVars)
		log.Printf("请设置这些环境变量以确保服务正常工作")
		
		// 使用默认值以便服务能够启动
		if appID == "" {
			appID = "8877631864"
		}
		if bearerToken == "" {
			bearerToken = "IZFPVWC5rVIoR5vRYyc21BdJI0qNanse"
		}
		if cluster == "" {
			cluster = "volcano_icl"
		}
		if voiceType == "" {
			voiceType = "S_JuVo3sao1"
		}
	}
	
	// 读取可选的环境变量，使用默认值如果未设置
	url := os.Getenv("BYTEDANCE_TTS_ENDPOINT")
	if url == "" {
		url = "https://openspeech.bytedance.com/api/v1/tts"
	} else {
		log.Printf("使用自定义字节跳动TTS端点: %s", url)
	}
		timeoutStr := os.Getenv("BYTEDANCE_TTS_TIMEOUT")
	timeout := 30 * time.Second
	if timeoutStr != "" {
		if parsedTimeout, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = parsedTimeout
			log.Printf("使用自定义超时设置: %v", timeout)
		} else {
			log.Printf("无效的超时设置 '%s'，使用默认值30s", timeoutStr)
		}
	}
	
	// 设置配置
	ttsConfig = ByteDanceTTSConfig{
		AppID:       appID,
		BearerToken: bearerToken,
		Cluster:     cluster,
		URL:         url,
		VoiceType:   voiceType,
		Timeout:     timeout,
	}
}

// 检查环境变量配置状态
func checkEnvironmentVariables() map[string]interface{} {
	// 检查必要的环境变量是否已设置
	requiredVars := map[string]bool{
		"BYTEDANCE_TTS_APP_ID":       os.Getenv("BYTEDANCE_TTS_APP_ID") != "",
		"BYTEDANCE_TTS_BEARER_TOKEN": os.Getenv("BYTEDANCE_TTS_BEARER_TOKEN") != "",
		"BYTEDANCE_TTS_CLUSTER":     os.Getenv("BYTEDANCE_TTS_CLUSTER") != "",
		"BYTEDANCE_TTS_VOICE_TYPE":  os.Getenv("BYTEDANCE_TTS_VOICE_TYPE") != "",
	}
	
	missingVars := []string{}
	for varName, isSet := range requiredVars {
		if !isSet {
			missingVars = append(missingVars, varName)
		}
	}
	
	// 检查可选的环境变量是否已设置
	optionalVars := map[string]bool{
		"BYTEDANCE_TTS_ENDPOINT": os.Getenv("BYTEDANCE_TTS_ENDPOINT") != "",
		"BYTEDANCE_TTS_TIMEOUT":  os.Getenv("BYTEDANCE_TTS_TIMEOUT") != "",
		"OPENAI_TTS_API_KEY":     os.Getenv("OPENAI_TTS_API_KEY") != "",
		"PORT":                   os.Getenv("PORT") != "",
	}
	
	// 构建配置状态响应
	return map[string]interface{}{
		"all_required_vars_set": len(missingVars) == 0,
		"missing_required_vars": missingVars,
		"required_vars":        requiredVars, // 只显示是否设置，不显示具体值
		"optional_vars":        optionalVars, // 只显示是否设置，不显示具体值
	}
}



func httpPost(url string, headers map[string]string, body []byte, timeout time.Duration) ([]byte, error) {
	client := &http.Client{
		Timeout: timeout,
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	retBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return retBody, err
}

func synthesis(text string, speed float64) ([]byte, error) {
	reqID := uuid.NewString()
	params := make(map[string]map[string]interface{})
	params["app"] = make(map[string]interface{})
	params["app"]["appid"] = ttsConfig.AppID
	params["app"]["token"] = "access_token"
	params["app"]["cluster"] = ttsConfig.Cluster

	params["user"] = make(map[string]interface{})
	params["user"]["uid"] = "uid"

	params["audio"] = make(map[string]interface{})
	params["audio"]["voice_type"] = ttsConfig.VoiceType
	params["audio"]["encoding"] = "wav"

	// 处理语速参数
	if speed <= 0 {
		speed = 1.0
	}
	params["audio"]["speed_ratio"] = speed
	params["audio"]["volume_ratio"] = 1.0
	params["audio"]["pitch_ratio"] = 1.0

	params["request"] = make(map[string]interface{})
	params["request"]["reqid"] = reqID
	params["request"]["text"] = text
	params["request"]["text_type"] = "plain"
	params["request"]["operation"] = "query"

	headers := make(map[string]string)
	headers["Content-Type"] = "application/json"
	headers["Authorization"] = fmt.Sprintf("Bearer;%s", ttsConfig.BearerToken)

	bodyStr, _ := json.Marshal(params)
	synResp, err := httpPost(ttsConfig.URL, headers, []byte(bodyStr), ttsConfig.Timeout)
	if err != nil {
		log.Printf("http post fail [err:%s]\n", err.Error())
		return nil, err
	}

	var respJSON TTSServResponse
	err = json.Unmarshal(synResp, &respJSON)
	if err != nil {
		log.Printf("unmarshal response fail [err:%s]\n", err.Error())
		return nil, err
	}

	if respJSON.Code != 3000 {
		log.Printf("code fail [code:%d, message:%s]\n", respJSON.Code, respJSON.Message)
		return nil, fmt.Errorf("TTS service error: code %d, message: %s", respJSON.Code, respJSON.Message)
	}

	audio, err := base64.StdEncoding.DecodeString(respJSON.Data)
	if err != nil {
		log.Printf("base64 decode fail [err:%s]\n", err.Error())
		return nil, err
	}
	return audio, nil
}

// 验证API密钥
func validateAPIKey(r *http.Request) bool {
	// 当VALID_API_KEY为空时，允许任何API密钥通过验证
	if VALID_API_KEY == "" {
		return true
	}

	// 从 Authorization header 中获取 Bearer token
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	// 检查是否以 "Bearer " 开头
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	// 提取token
	token := strings.TrimPrefix(authHeader, "Bearer ")
	return token == VALID_API_KEY
}

// OpenAI TTS API兼容端点
func openaiTTSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 验证API密钥
	if !validateAPIKey(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid API key provided.",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
		return
	}

	var req OpenAITTSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Input == "" {
		http.Error(w, "Input text is required", http.StatusBadRequest)
		return
	}

	// 设置默认语速
	speed := req.Speed
	if speed <= 0 {
		speed = 1.0
	}

	// 调用字节跳动TTS
	ttsStart := time.Now()
	audioData, err := synthesis(req.Input, speed)
	// 记录TTS处理时间
	_ = time.Since(ttsStart)

	if err != nil {
		log.Printf("TTS synthesis failed: %v", err)
		http.Error(w, "TTS synthesis failed", http.StatusInternalServerError)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(audioData)))

	// 返回音频数据
	w.WriteHeader(http.StatusOK)
	w.Write(audioData)
}

// 统计数据结构体
type Stats struct {
	totalRequests       int64
	successfulRequests  int64
	failedRequests      int64
	totalResponseTime   time.Duration
	recentResponseTimes []float64
	maxRecentResponses  int
	lastErrors          []string
	maxLastErrors       int
	mutex               sync.RWMutex
}

// API调用统计
var apiStats = &Stats{
	recentResponseTimes: make([]float64, 0, 100),
	maxRecentResponses:  100,
	lastErrors:          make([]string, 0, 10),
	maxLastErrors:       10,
}

// 添加请求统计
func addRequestStats(success bool, responseTime time.Duration, errMsg string) {
	apiStats.mutex.Lock()
	defer apiStats.mutex.Unlock()

	apiStats.totalRequests++
	apiStats.totalResponseTime += responseTime

	// 添加到最近响应时间数组
	apiStats.recentResponseTimes = append(apiStats.recentResponseTimes, responseTime.Seconds()*1000) // 转换为毫秒
	if len(apiStats.recentResponseTimes) > apiStats.maxRecentResponses {
		apiStats.recentResponseTimes = apiStats.recentResponseTimes[1:]
	}

	if success {
		apiStats.successfulRequests++
	} else {
		apiStats.failedRequests++
		// 添加到最近错误数组
		if errMsg != "" {
			errInfo := fmt.Sprintf("%s: %s", time.Now().Format(time.RFC3339), errMsg)
			apiStats.lastErrors = append(apiStats.lastErrors, errInfo)
			if len(apiStats.lastErrors) > apiStats.maxLastErrors {
				apiStats.lastErrors = apiStats.lastErrors[1:]
			}
		}
	}
}

// 获取内存信息
func getMemoryInfo() map[string]uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]uint64{
		"total":      m.Sys,
		"allocated":  m.Alloc,
		"available":  m.Sys - m.Alloc,
		"goroutines": uint64(runtime.NumGoroutine()),
	}
}

// 获取网络信息
func getNetworkInfo() map[string]interface{} {
	ifaces, err := net.Interfaces()
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}

	interfaces := make([]map[string]interface{}, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		addresses := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			addresses = append(addresses, addr.String())
		}
		interfaces = append(interfaces, map[string]interface{}{
			"name":      iface.Name,
			"mac":       iface.HardwareAddr.String(),
			"addresses": addresses,
			"up":        (iface.Flags & net.FlagUp) != 0,
		})
	}

	return map[string]interface{}{
		"interfaces": interfaces,
	}
}

// 健康检查端点
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// 收集统计数据
	apiStats.mutex.RLock()
	totalRequests := apiStats.totalRequests
	successfulRequests := apiStats.successfulRequests
	failedRequests := apiStats.failedRequests
	totalResponseTime := apiStats.totalResponseTime
	recentResponseTimes := make([]float64, len(apiStats.recentResponseTimes))
	copy(recentResponseTimes, apiStats.recentResponseTimes)
	lastErrors := make([]string, len(apiStats.lastErrors))
	copy(lastErrors, apiStats.lastErrors)
	apiStats.mutex.RUnlock()

	// 计算错误率
	var errorRate float64
	if totalRequests > 0 {
		errorRate = float64(failedRequests) / float64(totalRequests) * 100
	}

	// 计算平均响应时间
	var avgResponseTime float64
	if totalRequests > 0 {
		avgResponseTime = totalResponseTime.Seconds() * 1000 / float64(totalRequests) // 毫秒
	}

	// 检查环境变量配置
	envCheckStatus := checkEnvironmentVariables()
	allEnvVarsSet := envCheckStatus["all_required_vars_set"].(bool)
	
	// 确定服务状态
	status := "ok"
	if !allEnvVarsSet {
		status = "configuration_error"
	}

	// 构建响应
	response := map[string]interface{}{
		"status":     status,
		"service":    "ByteDance TTS to OpenAI API Adapter",
		"version":    "1.0.0",
		"uptime":     fmt.Sprintf("%.0f seconds", time.Since(startTime).Seconds()),
		"start_time": startTime.Format(time.RFC3339),
		"pid":        os.Getpid(),
		"memory":     getMemoryInfo(),
		"network":    getNetworkInfo(),
		"api_stats": map[string]interface{}{
			"total_requests":           totalRequests,
			"successful_requests":      successfulRequests,
			"failed_requests":          failedRequests,
			"error_rate":               fmt.Sprintf("%.2f%%", errorRate),
			"avg_response_time_ms":     fmt.Sprintf("%.2f", avgResponseTime),
			"recent_response_times_ms": recentResponseTimes,
			"concurrent_requests":      runtime.NumGoroutine() - 1, // 减去健康检查本身的goroutine
		},
		"errors": map[string]interface{}{
			"recent_errors": lastErrors,
		},
		"config_status": envCheckStatus,
	}

	json.NewEncoder(w).Encode(response)
}

var startTime time.Time

// 自定义ResponseWriter以捕获状态码
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func main() {
	startTime = time.Now()

	// 从环境变量读取API密钥，当环境变量未设置时，设为空字符串（允许任意key）
	VALID_API_KEY = os.Getenv("OPENAI_TTS_API_KEY")
	if VALID_API_KEY == "" {
		log.Println("Warning: OPENAI_TTS_API_KEY environment variable not set. All API keys will be accepted.")
	}
	
	// 初始化字节跳动TTS配置
	initTTSConfig()

	// 设置日志格式
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("[TTS-Server] ")

	router := mux.NewRouter()

	// 添加中间件：请求日志和统计
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 跳过健康检查的统计，避免递归
			if r.URL.Path == "/health" {
				start := time.Now()
				next.ServeHTTP(w, r)
				log.Printf("%s %s %s %v", r.Method, r.RequestURI, r.RemoteAddr, time.Since(start))
				return
			}

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)
			duration := time.Since(start)

			// 记录日志
			log.Printf("%s %s %s %d %v", r.Method, r.RequestURI, r.RemoteAddr, rec.statusCode, duration)

			// 更新统计
			success := rec.statusCode >= 200 && rec.statusCode < 400
			errMsg := ""
			if !success {
				errMsg = fmt.Sprintf("HTTP %d", rec.statusCode)
			}
			addRequestStats(success, duration, errMsg)
		})
	})

	// OpenAI TTS API兼容端点
	router.HandleFunc("/v1/audio/speech", openaiTTSHandler).Methods("POST")

	// 健康检查
	router.HandleFunc("/health", healthHandler).Methods("GET")

	// 根路径重定向到健康检查
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/health", http.StatusFound)
	}).Methods("GET")

	port := ":8080"
	// 从环境变量获取端口配置
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = ":" + envPort
	}

	server := &http.Server{
		Addr:         port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 创建一个通道来接收系统信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 在goroutine中启动服务器
	go func() {
		log.Printf("Starting ByteDance TTS to OpenAI API adapter server on port %s", port)
		log.Printf("OpenAI TTS endpoint: http://localhost%s/v1/audio/speech", port)
		log.Printf("Health check: http://localhost%s/health", port)
		log.Printf("PID: %d", os.Getpid())

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// 等待信号
	<-quit
	log.Println("Shutting down server...")

	// 创建一个5秒的超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 优雅关闭服务器
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	} else {
		log.Println("Server exited gracefully")
	}
}
