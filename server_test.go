package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	users    []user
	sessions map[string]user
	saved    *storedPlatformSettings
}

func (f *fakeStore) authenticate(username, password string) (user, error) {
	for _, u := range f.users {
		if u.Username == username && password == "member-pass-123" {
			return u, nil
		}
	}
	return user{}, errors.New("用户名或密码错误")
}

func (f *fakeStore) createSession(userID int64, _ time.Duration) (string, error) {
	for _, u := range f.users {
		if u.ID == userID {
			f.sessions["test-session"] = u
			return "test-session", nil
		}
	}
	return "", errors.New("用户不存在")
}

func (f *fakeStore) sessionUser(token string) (user, error) {
	u, ok := f.sessions[token]
	if !ok {
		return user{}, errors.New("登录已失效")
	}
	return u, nil
}

func (f *fakeStore) deleteSession(token string)                                  { delete(f.sessions, token) }
func (f *fakeStore) listUsers() ([]user, error)                                  { return f.users, nil }
func (f *fakeStore) createUser(string, string) (user, error)                     { return user{}, nil }
func (f *fakeStore) updateUser(int64, *int, string) error                        { return nil }
func (f *fakeStore) addUpload(context.Context, int64, string, int, bool, string) {}
func (f *fakeStore) listUploads() ([]map[string]any, error)                      { return []map[string]any{}, nil }
func (f *fakeStore) getPlatformSettings() (storedPlatformSettings, error) {
	return storedPlatformSettings{}, sql.ErrNoRows
}
func (f *fakeStore) savePlatformSettings(settings storedPlatformSettings) error {
	f.saved = &settings
	return nil
}

func newTestCookieJar(t *testing.T) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func TestNormalizeChannelEnforcesBaseURLPolicy(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		wantBaseURL string
	}{
		{name: "OpenAI must be empty", channelType: 1, wantBaseURL: ""},
		{name: "Anthropic uses OpenRouter", channelType: 14, wantBaseURL: anthropicBaseURL},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := map[string]any{"name": "test", "type": test.channelType, "key": "secret", "models": "model", "group": "default", "base_url": "https://attacker.invalid", "id": 99}
			name, channelType, err := normalizeChannel(channel)
			if err != nil {
				t.Fatalf("normalizeChannel() error = %v", err)
			}
			if name != "test" || channelType != test.channelType {
				t.Fatalf("unexpected identity: %q / %d", name, channelType)
			}
			if channel["base_url"] != test.wantBaseURL {
				t.Fatalf("base_url = %v, want %q", channel["base_url"], test.wantBaseURL)
			}
			if _, exists := channel["id"]; exists {
				t.Fatal("server-owned id was not removed")
			}
		})
	}
}

func TestUserCanCreateChannelButCannotManageUsers(t *testing.T) {
	var captured map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channel/" || r.Method != http.MethodPost {
			t.Fatalf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "personal-token" || r.Header.Get("New-Api-User") != "1" {
			t.Fatalf("missing upstream auth headers")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer upstream.Close()

	st := &fakeStore{users: []user{{ID: 2, Username: "member", Role: "user", Status: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339)}}, sessions: map[string]user{}}
	cfg := config{NewAPIBaseURL: upstream.URL, NewAPIAccessToken: "personal-token", NewAPIUserID: "1", SessionTTL: time.Hour}
	app := httptest.NewServer(newServer(cfg, st).routes())
	defer app.Close()

	client := &http.Client{Jar: newTestCookieJar(t)}
	loginBody := `{"username":"member","password":"member-pass-123"}`
	res, err := client.Post(app.URL+"/api/auth/login", "application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", res.StatusCode)
	}
	res.Body.Close()

	createBody := `{"mode":"single","channel":{"name":"anthropic-test","type":14,"key":"sk-upstream","models":"claude-3-5-sonnet","group":"default","base_url":"https://invalid.example"}}`
	res, err = client.Post(app.URL+"/api/channels", "application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create channel status = %d", res.StatusCode)
	}
	res.Body.Close()
	channel := captured["channel"].(map[string]any)
	if channel["base_url"] != anthropicBaseURL {
		t.Fatalf("upstream base_url = %v", channel["base_url"])
	}

	res, err = client.Get(app.URL + "/api/admin/users")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("admin route status = %d, want 403", res.StatusCode)
	}
}

func TestAdminCanSaveEncryptedNewAPISettings(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/group/":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []string{"default"}})
		case "/api/channel/models_enabled":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []string{"gpt-4o"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	st := &fakeStore{users: []user{{ID: 1, Username: "member", Role: "admin", Status: 1}}, sessions: map[string]user{}}
	cfg := config{SessionTTL: time.Hour, SettingsKey: bytes.Repeat([]byte{3}, 32)}
	app := httptest.NewServer(newServer(cfg, st).routes())
	defer app.Close()
	client := &http.Client{Jar: newTestCookieJar(t)}
	res, err := client.Post(app.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"member","password":"member-pass-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	body := `{"newapi_base_url":"` + upstream.URL + `","newapi_access_token":"secret-token","newapi_user_id":"1"}`
	req, err := http.NewRequest(http.MethodPut, app.URL+"/api/admin/settings", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("settings status = %d", res.StatusCode)
	}
	if st.saved == nil || st.saved.AccessTokenEncrypted == "secret-token" {
		t.Fatal("access token was not encrypted")
	}
	plaintext, err := decryptSecret(cfg.SettingsKey, st.saved.AccessTokenEncrypted)
	if err != nil || plaintext != "secret-token" {
		t.Fatalf("saved token decrypt failed: %v", err)
	}
}
