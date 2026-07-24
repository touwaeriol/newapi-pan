package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const anthropicBaseURL = "https://openrouter.ai/api"

type newAPIClient struct {
	mu                           sync.RWMutex
	baseURL, accessToken, userID string
	httpClient                   *http.Client
}

type upstreamResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newClient(cfg config) *newAPIClient {
	return &newAPIClient{baseURL: cfg.NewAPIBaseURL, accessToken: cfg.NewAPIAccessToken, userID: cfg.NewAPIUserID, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

func (c *newAPIClient) configured() bool {
	baseURL, accessToken, userID := c.connection()
	return baseURL != "" && accessToken != "" && userID != ""
}

func (c *newAPIClient) configure(baseURL, accessToken, userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	c.accessToken = strings.TrimSpace(accessToken)
	c.userID = strings.TrimSpace(userID)
}

func (c *newAPIClient) connection() (string, string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.accessToken, c.userID
}

func (c *newAPIClient) request(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	baseURL, accessToken, userID := c.connection()
	if baseURL == "" || accessToken == "" || userID == "" {
		return nil, errors.New("服务端尚未配置 New API 地址、个人密钥和用户 ID")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", accessToken)
	req.Header.Set("New-Api-User", userID)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 New API 失败: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var payload upstreamResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("New API 返回无效响应（HTTP %d）", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || !payload.Success {
		message := strings.TrimSpace(payload.Message)
		if message == "" {
			message = fmt.Sprintf("New API 请求失败（HTTP %d）", res.StatusCode)
		}
		return nil, errors.New(message)
	}
	return payload.Data, nil
}

func (c *newAPIClient) metadata(ctx context.Context) (map[string]any, error) {
	groupsData, err := c.request(ctx, http.MethodGet, "/api/group/", nil)
	if err != nil {
		return nil, err
	}
	var groups []string
	if err := json.Unmarshal(groupsData, &groups); err != nil {
		return nil, errors.New("New API 分组响应格式不兼容")
	}
	models := make([]string, 0)
	if modelsData, modelsErr := c.request(ctx, http.MethodGet, "/api/channel/models_enabled", nil); modelsErr == nil {
		_ = json.Unmarshal(modelsData, &models)
	}
	allModels := make([]map[string]any, 0)
	if modelsData, modelsErr := c.request(ctx, http.MethodGet, "/api/channel/models", nil); modelsErr == nil {
		_ = json.Unmarshal(modelsData, &allModels)
	}
	prefillGroups := make([]map[string]any, 0)
	if prefillData, prefillErr := c.request(ctx, http.MethodGet, "/api/prefill_group/?type=model", nil); prefillErr == nil {
		_ = json.Unmarshal(prefillData, &prefillGroups)
	}
	return map[string]any{"groups": groups, "models": models, "all_models": allModels, "prefill_groups": prefillGroups}, nil
}

func normalizeChannelBaseURL(channelType int, baseURL string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if channelType != 14 {
		return "", nil
	}
	if baseURL != "" && baseURL != anthropicBaseURL {
		return "", errors.New("Anthropic Base URL 只允许为空或 https://openrouter.ai/api")
	}
	return baseURL, nil
}

func normalizeChannel(channel map[string]any) (string, int, error) {
	name, _ := channel["name"].(string)
	name = strings.TrimSpace(name)
	key, _ := channel["key"].(string)
	models, _ := channel["models"].(string)
	group, _ := channel["group"].(string)
	typeNumber, ok := numberAsInt(channel["type"])
	if name == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(models) == "" || strings.TrimSpace(group) == "" || !ok || typeNumber < 1 || typeNumber > 58 {
		return "", 0, errors.New("渠道名称、类型、密钥、模型和分组均为必填项")
	}
	for _, field := range []string{"id", "created_time", "used_quota", "balance", "test_time", "response_time"} {
		delete(channel, field)
	}
	channel["name"] = name
	channel["type"] = typeNumber
	requestedBaseURL, _ := channel["base_url"].(string)
	normalizedBaseURL, err := normalizeChannelBaseURL(typeNumber, requestedBaseURL)
	if err != nil {
		return "", 0, err
	}
	channel["base_url"] = normalizedBaseURL
	defaults := map[string]any{"status": 1, "priority": 0, "weight": 0, "auto_ban": 1}
	for key, value := range defaults {
		if _, exists := channel[key]; !exists {
			channel[key] = value
		}
	}
	return name, typeNumber, nil
}

func numberAsInt(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		if number != float64(int(number)) {
			return 0, false
		}
		return int(number), true
	case int:
		return number, true
	case json.Number:
		parsed, err := strconv.Atoi(number.String())
		return parsed, err == nil
	default:
		return 0, false
	}
}

func (c *newAPIClient) createChannel(ctx context.Context, request map[string]any) error {
	_, err := c.request(ctx, http.MethodPost, "/api/channel/", request)
	return err
}

func (c *newAPIClient) fetchModels(ctx context.Context, channelType int, key, baseURL string) ([]string, error) {
	baseURL, err := normalizeChannelBaseURL(channelType, baseURL)
	if err != nil {
		return nil, err
	}
	data, err := c.request(ctx, http.MethodPost, "/api/channel/fetch_models", map[string]any{
		"type": channelType, "key": strings.TrimSpace(key), "base_url": baseURL,
	})
	if err != nil {
		return nil, err
	}
	var models []string
	if err := json.Unmarshal(data, &models); err != nil {
		return nil, errors.New("New API 上游模型响应格式不兼容")
	}
	return models, nil
}
