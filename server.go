package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type server struct {
	cfg    config
	store  storeAPI
	newAPI *newAPIClient
	web    http.Handler
}

type storeAPI interface {
	authenticate(string, string) (user, error)
	createSession(int64, time.Duration) (string, error)
	sessionUser(string) (user, error)
	deleteSession(string)
	listUsers() ([]user, error)
	createUser(string, string) (user, error)
	updateUser(int64, *int, string) error
	addUpload(context.Context, int64, string, int, bool, string)
	listUploads() ([]map[string]any, error)
	getPlatformSettings() (storedPlatformSettings, error)
	savePlatformSettings(storedPlatformSettings) error
}

type contextKey string

const userContextKey contextKey = "user"

func newServer(cfg config, st storeAPI) *server {
	assets, _ := fs.Sub(webFiles, "web")
	client := newClient(cfg)
	if saved, err := st.getPlatformSettings(); err == nil {
		accessToken, decryptErr := decryptSecret(cfg.SettingsKey, saved.AccessTokenEncrypted)
		if decryptErr != nil {
			log.Printf("读取 New API 配置失败: %v", decryptErr)
		} else {
			client.configure(saved.BaseURL, accessToken, saved.UserID)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("读取平台配置失败: %v", err)
	}
	return &server{cfg: cfg, store: st, newAPI: client, web: http.FileServer(http.FS(assets))}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.withAuth(s.logout))
	mux.HandleFunc("GET /api/me", s.withAuth(s.me))
	mux.HandleFunc("GET /api/platform", s.withAuth(s.platform))
	mux.HandleFunc("GET /api/newapi/metadata", s.withAuth(s.metadata))
	mux.HandleFunc("POST /api/newapi/fetch-models", s.withAuth(s.fetchModels))
	mux.HandleFunc("POST /api/channels", s.withAuth(s.createChannel))
	mux.HandleFunc("GET /api/admin/users", s.withAdmin(s.listUsers))
	mux.HandleFunc("POST /api/admin/users", s.withAdmin(s.createUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", s.withAdmin(s.updateUser))
	mux.HandleFunc("GET /api/admin/uploads", s.withAdmin(s.listUploads))
	mux.HandleFunc("GET /api/admin/settings", s.withAdmin(s.getAdminSettings))
	mux.HandleFunc("PUT /api/admin/settings", s.withAdmin(s.updateAdminSettings))
	mux.Handle("/", s.web)
	return s.securityHeaders(s.requestLogger(mux))
}

func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r.Host) {
				writeError(w, http.StatusForbidden, "请求来源校验失败")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin, host string) bool {
	return origin == "http://"+host || origin == "https://"+host
}

func (s *server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
		}
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return errors.New("请求 JSON 格式错误")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": data})
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"success": false, "message": message})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.store.authenticate(body.Username, body.Password)
	if err != nil {
		time.Sleep(250 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	token, err := s.store.createSession(u.ID, s.cfg.SessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建登录会话失败")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(s.cfg.SessionTTL.Seconds())})
	writeOK(w, u)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		s.store.deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeOK(w, nil)
}

func (s *server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		u, err := s.store.sessionUser(cookie.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "登录已失效")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey, u)))
	}
}

func (s *server) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Role != "admin" {
			writeError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next(w, r)
	})
}

func currentUser(r *http.Request) user { return r.Context().Value(userContextKey).(user) }

func (s *server) me(w http.ResponseWriter, r *http.Request) { writeOK(w, currentUser(r)) }

func (s *server) platform(w http.ResponseWriter, _ *http.Request) {
	baseURL, _, _ := s.newAPI.connection()
	writeOK(w, map[string]any{"newapi_configured": s.newAPI.configured(), "newapi_base_url": baseURL, "anthropic_base_url": anthropicBaseURL, "channel_types": channelTypes})
}

func (s *server) getAdminSettings(w http.ResponseWriter, _ *http.Request) {
	baseURL, accessToken, userID := s.newAPI.connection()
	writeOK(w, map[string]any{"newapi_base_url": baseURL, "has_access_token": accessToken != "", "configured": baseURL != "" && accessToken != "" && userID != ""})
}

func (s *server) updateAdminSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BaseURL     string `json:"newapi_base_url"`
		AccessToken string `json:"newapi_access_token"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.BaseURL = strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	body.AccessToken = strings.TrimSpace(body.AccessToken)
	_, currentToken, userID := s.newAPI.connection()
	if body.AccessToken == "" {
		body.AccessToken = currentToken
	}
	if userID == "" {
		userID = strings.TrimSpace(s.cfg.NewAPIUserID)
	}
	if userID == "" {
		userID = "1"
	}
	parsedURL, err := url.ParseRequestURI(body.BaseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		writeError(w, http.StatusBadRequest, "New API 地址必须是有效的 HTTP(S) URL")
		return
	}
	if body.AccessToken == "" {
		writeError(w, http.StatusBadRequest, "个人密钥为必填项")
		return
	}
	testClient := newClient(config{NewAPIBaseURL: body.BaseURL, NewAPIAccessToken: body.AccessToken, NewAPIUserID: userID})
	metadata, err := testClient.metadata(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "连接验证失败: "+err.Error())
		return
	}
	encryptedToken, err := encryptSecret(s.cfg.SettingsKey, body.AccessToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "加密个人密钥失败")
		return
	}
	if err := s.store.savePlatformSettings(storedPlatformSettings{BaseURL: body.BaseURL, AccessTokenEncrypted: encryptedToken, UserID: userID}); err != nil {
		writeError(w, http.StatusInternalServerError, "保存配置失败")
		return
	}
	s.newAPI.configure(body.BaseURL, body.AccessToken, userID)
	writeOK(w, map[string]any{"message": "配置已保存并验证", "groups": metadata["groups"], "models": metadata["models"]})
}

func (s *server) metadata(w http.ResponseWriter, r *http.Request) {
	data, err := s.newAPI.metadata(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w, data)
}

func (s *server) fetchModels(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type    int    `json:"type"`
		Key     string `json:"key"`
		BaseURL string `json:"base_url"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Type < 1 || body.Type > 58 || strings.TrimSpace(body.Key) == "" {
		writeError(w, http.StatusBadRequest, "渠道类型和密钥为必填项")
		return
	}
	models, err := s.newAPI.fetchModels(r.Context(), body.Type, body.Key, body.BaseURL)
	if err != nil {
		if strings.Contains(err.Error(), "Base URL") {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return
	}
	writeOK(w, models)
}

func (s *server) createChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode                      string         `json:"mode"`
		MultiKeyMode              string         `json:"multi_key_mode"`
		BatchAddSetKeyPrefix2Name bool           `json:"batch_add_set_key_prefix_2_name"`
		Channel                   map[string]any `json:"channel"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, channelType, err := normalizeChannel(body.Channel)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Mode == "" {
		body.Mode = "single"
	}
	if body.Mode != "single" && body.Mode != "batch" && body.Mode != "multi_to_single" {
		writeError(w, http.StatusBadRequest, "无效创建模式")
		return
	}
	request := map[string]any{"mode": body.Mode, "multi_key_mode": body.MultiKeyMode, "batch_add_set_key_prefix_2_name": body.BatchAddSetKeyPrefix2Name, "channel": body.Channel}
	err = s.newAPI.createChannel(r.Context(), request)
	u := currentUser(r)
	message := "创建成功"
	if err != nil {
		message = err.Error()
	}
	s.store.addUpload(r.Context(), u.ID, name, channelType, err == nil, message)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeOK(w, map[string]any{"message": "渠道已创建", "base_url": body.Channel["base_url"]})
}

func (s *server) listUsers(w http.ResponseWriter, _ *http.Request) {
	users, err := s.store.listUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户失败")
		return
	}
	writeOK(w, users)
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.store.createUser(body.Username, body.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, u)
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "无效用户 ID")
		return
	}
	var body struct {
		Status   *int   `json:"status"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.updateUser(id, body.Status, body.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (s *server) listUploads(w http.ResponseWriter, _ *http.Request) {
	items, err := s.store.listUploads()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取上传记录失败")
		return
	}
	writeOK(w, items)
}

var channelTypes = []map[string]any{
	{"id": 1, "name": "OpenAI"}, {"id": 2, "name": "Midjourney"}, {"id": 3, "name": "Azure"}, {"id": 4, "name": "Ollama"},
	{"id": 5, "name": "MidjourneyPlus"}, {"id": 6, "name": "OpenAIMax"}, {"id": 7, "name": "OhMyGPT"}, {"id": 8, "name": "Custom"},
	{"id": 9, "name": "AILS"}, {"id": 10, "name": "AIProxy"}, {"id": 11, "name": "PaLM"}, {"id": 12, "name": "API2GPT"},
	{"id": 13, "name": "AIGC2D"}, {"id": 14, "name": "Anthropic"}, {"id": 15, "name": "Baidu"}, {"id": 16, "name": "Zhipu"},
	{"id": 17, "name": "Ali"}, {"id": 18, "name": "Xunfei"}, {"id": 19, "name": "360"}, {"id": 20, "name": "OpenRouter"},
	{"id": 21, "name": "AIProxyLibrary"}, {"id": 22, "name": "FastGPT"}, {"id": 23, "name": "Tencent"}, {"id": 24, "name": "Gemini"},
	{"id": 25, "name": "Moonshot"}, {"id": 26, "name": "ZhipuV4"}, {"id": 27, "name": "Perplexity"}, {"id": 31, "name": "LingYiWanWu"},
	{"id": 33, "name": "AWS"}, {"id": 34, "name": "Cohere"}, {"id": 35, "name": "MiniMax"}, {"id": 36, "name": "SunoAPI"},
	{"id": 37, "name": "Dify"}, {"id": 38, "name": "Jina"}, {"id": 39, "name": "Cloudflare"}, {"id": 40, "name": "SiliconFlow"},
	{"id": 41, "name": "VertexAI"}, {"id": 42, "name": "Mistral"}, {"id": 43, "name": "DeepSeek"}, {"id": 44, "name": "MokaAI"},
	{"id": 45, "name": "VolcEngine"}, {"id": 46, "name": "BaiduV2"}, {"id": 47, "name": "Xinference"}, {"id": 48, "name": "xAI"},
	{"id": 49, "name": "Coze"}, {"id": 50, "name": "Kling"}, {"id": 51, "name": "Jimeng"}, {"id": 52, "name": "Vidu"},
	{"id": 53, "name": "Submodel"}, {"id": 54, "name": "DoubaoVideo"}, {"id": 55, "name": "Sora"}, {"id": 56, "name": "Replicate"},
	{"id": 57, "name": "Codex"}, {"id": 58, "name": "Advanced Custom"},
}

func (s *server) listenAndServe() error {
	httpServer := &http.Server{Addr: ":" + s.cfg.Port, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("newapi-upload listening on http://127.0.0.1:%s", s.cfg.Port)
	return httpServer.ListenAndServe()
}
