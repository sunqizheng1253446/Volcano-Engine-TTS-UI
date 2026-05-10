package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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

const (
	DEFAULT_PORT          = "8080"
	DEFAULT_TIMEOUT       = 30 * time.Second
	MAX_TEXT_LENGTH       = 5000
	MIN_SPEED             = 0.25
	MAX_SPEED             = 4.0
	DEFAULT_SPEED         = 1.0
	MAX_REQUEST_BODY_SIZE = 1024 * 1024
	RATE_LIMIT_REQUESTS   = 100
	RATE_LIMIT_WINDOW     = time.Minute
	MAX_RESPONSE_TIMES    = 100
	MAX_ERRORS            = 10
)

type TTSServResponse struct {
	ReqID     string `json:"reqid"`
	Code      int    `json:"code"`
	Message   string `json:"Message"`
	Operation string `json:"operation"`
	Sequence  int    `json:"sequence"`
	Data      string `json:"data"`
}

type OpenAITTSRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

type ByteDanceTTSConfig struct {
	AppID       string
	BearerToken string
	Cluster     string
	URL         string
	VoiceType   string
	Timeout     time.Duration
}

type RateLimiter struct {
	requests map[string][]time.Time
	mutex    sync.Mutex
	limit    int
	window   time.Duration
}

type Stats struct {
	totalRequests       int64
	successfulRequests  int64
	failedRequests      int64
	totalResponseTime   time.Duration
	recentResponseTimes []float64
	responseTimesIndex  int
	lastErrors          []string
	errorsIndex         int
	mutex               sync.RWMutex
}

var (
	VALID_API_KEYS   []string
	ttsConfig        ByteDanceTTSConfig
	globalHTTPClient *http.Client
	apiStats         *Stats
	rateLimiter      *RateLimiter
)

func init() {
	globalHTTPClient = &http.Client{
		Timeout: DEFAULT_TIMEOUT,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	apiStats = &Stats{
		recentResponseTimes: make([]float64, MAX_RESPONSE_TIMES),
		lastErrors:          make([]string, MAX_ERRORS),
	}

	rateLimiter = &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    RATE_LIMIT_REQUESTS,
		window:   RATE_LIMIT_WINDOW,
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	timestamps := rl.requests[key]
	valid := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= rl.limit {
		rl.requests[key] = valid
		return false
	}

	valid = append(valid, now)
	rl.requests[key] = valid
	return true
}

func initTTSConfig() error {
	appID := os.Getenv("BYTEDANCE_TTS_APP_ID")
	bearerToken := os.Getenv("BYTEDANCE_TTS_BEARER_TOKEN")
	cluster := os.Getenv("BYTEDANCE_TTS_CLUSTER")
	voiceType := os.Getenv("BYTEDANCE_TTS_VOICE_TYPE")

	missingVars := []string{}
	if appID == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_APP_ID")
	}
	if bearerToken == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_BEARER_TOKEN")
	}
	if cluster == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_CLUSTER")
	}
	if voiceType == "" {
		missingVars = append(missingVars, "BYTEDANCE_TTS_VOICE_TYPE")
	}

	if len(missingVars) > 0 {
		return fmt.Errorf("缺少必需的环境变量: %v", missingVars)
	}

	url := os.Getenv("BYTEDANCE_TTS_ENDPOINT")
	if url == "" {
		url = "https://openspeech.bytedance.com/api/v1/tts"
	}

	timeout := DEFAULT_TIMEOUT
	if timeoutStr := os.Getenv("BYTEDANCE_TTS_TIMEOUT"); timeoutStr != "" {
		if parsedTimeout, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = parsedTimeout
		} else {
			log.Printf("无效的超时设置 '%s'，使用默认值: %v", timeoutStr, timeout)
		}
	}

	ttsConfig = ByteDanceTTSConfig{
		AppID:       appID,
		BearerToken: bearerToken,
		Cluster:     cluster,
		URL:         url,
		VoiceType:   voiceType,
		Timeout:     timeout,
	}

	return nil
}

func initAPIKeys() {
	apiKey := os.Getenv("OPENAI_TTS_API_KEY")
	if apiKey != "" {
		VALID_API_KEYS = strings.Split(apiKey, ",")
		for i, k := range VALID_API_KEYS {
			VALID_API_KEYS[i] = strings.TrimSpace(k)
		}
		log.Printf("已配置 %d 个有效的API密钥", len(VALID_API_KEYS))
	} else {
		log.Println("警告: OPENAI_TTS_API_KEY 环境变量未设置，将允许所有请求")
	}
}

func checkEnvironmentVariables() map[string]interface{} {
	requiredVars := map[string]bool{
		"BYTEDANCE_TTS_APP_ID":       os.Getenv("BYTEDANCE_TTS_APP_ID") != "",
		"BYTEDANCE_TTS_BEARER_TOKEN": os.Getenv("BYTEDANCE_TTS_BEARER_TOKEN") != "",
		"BYTEDANCE_TTS_CLUSTER":      os.Getenv("BYTEDANCE_TTS_CLUSTER") != "",
		"BYTEDANCE_TTS_VOICE_TYPE":   os.Getenv("BYTEDANCE_TTS_VOICE_TYPE") != "",
	}

	missingVars := []string{}
	for varName, isSet := range requiredVars {
		if !isSet {
			missingVars = append(missingVars, varName)
		}
	}

	optionalVars := map[string]bool{
		"BYTEDANCE_TTS_ENDPOINT": os.Getenv("BYTEDANCE_TTS_ENDPOINT") != "",
		"BYTEDANCE_TTS_TIMEOUT":  os.Getenv("BYTEDANCE_TTS_TIMEOUT") != "",
		"OPENAI_TTS_API_KEY":     os.Getenv("OPENAI_TTS_API_KEY") != "",
		"PORT":                   os.Getenv("PORT") != "",
	}

	return map[string]interface{}{
		"all_required_vars_set": len(missingVars) == 0,
		"missing_required_vars": missingVars,
		"required_vars_set":     requiredVars,
		"optional_vars_set":     optionalVars,
	}
}

func httpPost(url string, headers map[string]string, body []byte, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := globalHTTPClient
	if timeout != 0 && timeout != ttsConfig.Timeout {
		client = &http.Client{Timeout: timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	retBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return retBody, nil
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

	bodyStr, err := json.Marshal(params)
	if err != nil {
		log.Printf("JSON marshal fail: %v", err)
		return nil, err
	}

	synResp, err := httpPost(ttsConfig.URL, headers, bodyStr, ttsConfig.Timeout)
	if err != nil {
		log.Printf("http post fail: %v", err)
		return nil, err
	}

	var respJSON TTSServResponse
	err = json.Unmarshal(synResp, &respJSON)
	if err != nil {
		log.Printf("unmarshal response fail: %v", err)
		return nil, err
	}

	if respJSON.Code != 3000 {
		log.Printf("TTS service error: code=%d, message=%s", respJSON.Code, respJSON.Message)
		return nil, fmt.Errorf("TTS service error")
	}

	audio, err := base64.StdEncoding.DecodeString(respJSON.Data)
	if err != nil {
		log.Printf("base64 decode fail: %v", err)
		return nil, err
	}
	return audio, nil
}

func validateAPIKey(r *http.Request) bool {
	if len(VALID_API_KEYS) == 0 {
		return true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	for _, validKey := range VALID_API_KEYS {
		if token == validKey {
			return true
		}
	}
	return false
}

func getClientIP(r *http.Request) string {
	xForwardedFor := r.Header.Get("X-Forwarded-For")
	if xForwardedFor != "" {
		ips := strings.Split(xForwardedFor, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	xRealIP := r.Header.Get("X-Real-IP")
	if xRealIP != "" {
		return xRealIP
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func openaiTTSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	clientIP := getClientIP(r)
	if !rateLimiter.Allow(clientIP) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Rate limit exceeded. Please try again later.",
				"type":    "rate_limit_error",
				"code":    "rate_limit_exceeded",
			},
		})
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MAX_REQUEST_BODY_SIZE))
	if err != nil {
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var req OpenAITTSRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Input == "" {
		http.Error(w, "Input text is required", http.StatusBadRequest)
		return
	}

	if len(req.Input) > MAX_TEXT_LENGTH {
		http.Error(w, fmt.Sprintf("Input text too long (max %d characters)", MAX_TEXT_LENGTH), http.StatusBadRequest)
		return
	}

	speed := req.Speed
	if speed <= 0 {
		speed = DEFAULT_SPEED
	}
	if speed < MIN_SPEED {
		speed = MIN_SPEED
	}
	if speed > MAX_SPEED {
		speed = MAX_SPEED
	}

	ttsStart := time.Now()
	audioData, err := synthesis(req.Input, speed)
	duration := time.Since(ttsStart)

	if err != nil {
		addRequestStats(false, duration, err.Error())
		http.Error(w, "TTS synthesis failed", http.StatusInternalServerError)
		return
	}

	addRequestStats(true, duration, "")

	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(audioData)))
	w.WriteHeader(http.StatusOK)
	w.Write(audioData)
}

func addRequestStats(success bool, responseTime time.Duration, errMsg string) {
	apiStats.mutex.Lock()
	defer apiStats.mutex.Unlock()

	apiStats.totalRequests++
	apiStats.totalResponseTime += responseTime

	apiStats.recentResponseTimes[apiStats.responseTimesIndex] = responseTime.Seconds() * 1000
	apiStats.responseTimesIndex = (apiStats.responseTimesIndex + 1) % MAX_RESPONSE_TIMES

	if success {
		apiStats.successfulRequests++
	} else {
		apiStats.failedRequests++
		if errMsg != "" {
			errInfo := fmt.Sprintf("%s: %s", time.Now().Format(time.RFC3339), errMsg)
			apiStats.lastErrors[apiStats.errorsIndex] = errInfo
			apiStats.errorsIndex = (apiStats.errorsIndex + 1) % MAX_ERRORS
		}
	}
}

func getMemoryInfo() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]interface{}{
		"total_alloc": m.TotalAlloc,
		"heap_alloc":  m.HeapAlloc,
		"heap_inuse":  m.HeapInuse,
		"goroutines":  runtime.NumGoroutine(),
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	apiStats.mutex.RLock()
	totalRequests := apiStats.totalRequests
	successfulRequests := apiStats.successfulRequests
	failedRequests := apiStats.failedRequests
	totalResponseTime := apiStats.totalResponseTime
	recentResponseTimes := make([]float64, 0, MAX_RESPONSE_TIMES)
	for _, t := range apiStats.recentResponseTimes {
		if t > 0 {
			recentResponseTimes = append(recentResponseTimes, t)
		}
	}
	lastErrors := make([]string, 0, MAX_ERRORS)
	for _, e := range apiStats.lastErrors {
		if e != "" {
			lastErrors = append(lastErrors, e)
		}
	}
	apiStats.mutex.RUnlock()

	var errorRate float64
	if totalRequests > 0 {
		errorRate = float64(failedRequests) / float64(totalRequests) * 100
	}

	var avgResponseTime float64
	if totalRequests > 0 {
		avgResponseTime = totalResponseTime.Seconds() * 1000 / float64(totalRequests)
	}

	envCheckStatus := checkEnvironmentVariables()
	allEnvVarsSet := envCheckStatus["all_required_vars_set"].(bool)

	status := "ok"
	if !allEnvVarsSet {
		status = "configuration_error"
	}

	response := map[string]interface{}{
		"status":     status,
		"service":    "ByteDance TTS to OpenAI API Adapter",
		"version":    "1.1.0",
		"uptime":     fmt.Sprintf("%.0f seconds", time.Since(startTime).Seconds()),
		"start_time": startTime.Format(time.RFC3339),
		"memory":     getMemoryInfo(),
		"api_stats": map[string]interface{}{
			"total_requests":           totalRequests,
			"successful_requests":      successfulRequests,
			"failed_requests":          failedRequests,
			"error_rate_percent":       fmt.Sprintf("%.2f", errorRate),
			"avg_response_time_ms":     fmt.Sprintf("%.2f", avgResponseTime),
			"recent_response_times_ms": recentResponseTimes,
		},
		"errors": map[string]interface{}{
			"recent_errors_count": len(lastErrors),
		},
		"config_status": map[string]interface{}{
			"all_required_vars_set": allEnvVarsSet,
		},
	}

	json.NewEncoder(w).Encode(response)
}

var startTime time.Time

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	startTime = time.Now()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("[TTS-Server] ")

	initAPIKeys()

	if err := initTTSConfig(); err != nil {
		log.Fatalf("配置初始化失败: %v", err)
	}

	router := mux.NewRouter()

	router.Use(corsMiddleware)

	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			log.Printf("%s %s %s %d %v", r.Method, r.RequestURI, r.RemoteAddr, rec.statusCode, duration)
		})
	})

	router.HandleFunc("/v1/audio/speech", openaiTTSHandler).Methods("POST", "OPTIONS")
	router.HandleFunc("/health", healthHandler).Methods("GET")
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/health", http.StatusFound)
	}).Methods("GET")

	port := os.Getenv("PORT")
	if port == "" {
		port = DEFAULT_PORT
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Starting ByteDance TTS to OpenAI API adapter server on port %s", port)
		log.Printf("OpenAI TTS endpoint: http://localhost:%s/v1/audio/speech", port)
		log.Printf("Health check: http://localhost:%s/health", port)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	} else {
		log.Println("Server exited gracefully")
	}
}
