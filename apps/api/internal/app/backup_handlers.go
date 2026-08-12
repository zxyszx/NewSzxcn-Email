package app

import (
	"archive/tar"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
)

const backupTelegramLimit = 49 << 20

type backupJob struct {
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	Error     string    `json:"error,omitempty"`
}

type backupItem struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	SHA256    string    `json:"sha256,omitempty"`
}

type backupListResponse struct {
	Enabled       bool              `json:"enabled"`
	TelegramSet   bool              `json:"telegramSet"`
	TelegramLimit int64             `json:"telegramLimit"`
	Job           *backupJob        `json:"job,omitempty"`
	Items         []backupItem      `json:"items"`
	Schedule      backupSchedule    `json:"schedule"`
	GoogleDrive   googleDriveStatus `json:"googleDrive"`
}

type createBackupRequest struct {
	Password          string `json:"password"`
	ConfirmPassword   string `json:"confirmPassword"`
	SendTelegram      bool   `json:"sendTelegram"`
	UploadGoogleDrive bool   `json:"uploadGoogleDrive"`
}

type backupSchedule struct {
	Enabled            bool   `json:"enabled"`
	Days               int    `json:"days"`
	PasswordSet        bool   `json:"passwordSet"`
	PasswordHint       string `json:"passwordHint,omitempty"`
	ServerIP           string `json:"serverIp"`
	ChatID             string `json:"chatId"`
	TelegramMode       string `json:"telegramMode"`
	TelegramEnabled    bool   `json:"telegramEnabled"`
	GoogleDriveEnabled bool   `json:"googleDriveEnabled"`
}

func detectPublicServerIP(ctx context.Context, hostname string) string {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return ""
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if isPublicIP(ip) {
			return ip.String()
		}
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, hostname)
	if err != nil {
		return ""
	}
	var ipv6 string
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			continue
		}
		if address.IP.To4() != nil {
			return address.IP.String()
		}
		if ipv6 == "" {
			ipv6 = address.IP.String()
		}
	}
	return ipv6
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

type updateBackupScheduleRequest struct {
	Enabled            bool   `json:"enabled"`
	Days               int    `json:"days"`
	Password           string `json:"password"`
	ConfirmPassword    string `json:"confirmPassword"`
	ServerIP           string `json:"serverIp"`
	ChatID             string `json:"chatId"`
	TelegramMode       string `json:"telegramMode"`
	TelegramEnabled    bool   `json:"telegramEnabled"`
	GoogleDriveEnabled bool   `json:"googleDriveEnabled"`
	GoogleClientID     string `json:"googleClientId"`
	GoogleClientSecret string `json:"googleClientSecret"`
	GoogleFolderName   string `json:"googleFolderName"`
}

type updateBackupPasswordRequest struct {
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
}

type testBackupTelegramRequest struct {
	Mode   string `json:"mode"`
	ChatID string `json:"chatId"`
}

type googleDriveStatus struct {
	ClientID        string `json:"clientId"`
	ClientSecretSet bool   `json:"clientSecretSet"`
	Connected       bool   `json:"connected"`
	FolderName      string `json:"folderName"`
}

type googleDriveOAuthState struct {
	ExpiresAt int64  `json:"expiresAt"`
	Nonce     string `json:"nonce"`
}

type googleDriveToken struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	Expiry       time.Time `json:"expiry"`
}

func (a *App) requireSystemAdmin(w http.ResponseWriter, r *http.Request) bool {
	user := currentUser(r)
	if user == nil || user.Role != "admin" {
		respondError(w, http.StatusForbidden, "system administrator required")
		return false
	}
	return true
}

func (a *App) handleListBackups(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	items, err := a.listBackups()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "无法读取备份列表")
		return
	}
	a.backupMu.Lock()
	job := a.backupJob
	if job != nil {
		copy := *job
		job = &copy
	}
	a.backupMu.Unlock()
	schedule, _ := a.loadBackupSchedule(r.Context())
	schedule.ServerIP = detectPublicServerIP(r.Context(), a.config().PublicHostname)
	telegramToken, telegramDestination, _ := a.backupTelegramCredentials(r.Context(), schedule)
	respondJSON(w, http.StatusOK, backupListResponse{
		Enabled:       a.backupAssetsAvailable(),
		TelegramSet:   strings.TrimSpace(telegramToken) != "" && validTelegramPrivateChatID(telegramDestination),
		TelegramLimit: backupTelegramLimit, Job: job, Items: items, Schedule: schedule,
		GoogleDrive: a.loadGoogleDriveStatus(r.Context()),
	})
}

func (a *App) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	var req createBackupRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	a.backupMu.Lock()
	locked := true
	defer func() {
		if locked {
			a.backupMu.Unlock()
		}
	}()
	if a.backupJob != nil && a.backupJob.Status == "running" {
		respondError(w, http.StatusConflict, "已有备份任务正在运行")
		return
	}
	password, err := a.savedBackupPassword(r.Context())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		respondError(w, http.StatusInternalServerError, "无法读取已保存的备份密码")
		return
	}
	if password == "" {
		if !validBackupPassword(req.Password) {
			badRequest(w, errors.New("首次创建备份时，密码至少需要 8 个字符"))
			return
		}
		if req.Password != req.ConfirmPassword {
			badRequest(w, errors.New("两次输入的备份密码不一致"))
			return
		}
	}
	if !a.backupAssetsAvailable() {
		respondError(w, http.StatusServiceUnavailable, "当前部署尚未启用完整备份")
		return
	}
	if password == "" {
		ciphertext, encryptErr := a.encryptBackupPassword(req.Password)
		if encryptErr != nil {
			respondError(w, http.StatusInternalServerError, "无法安全保存备份密码")
			return
		}
		now := a.now().UTC().Format(time.RFC3339Nano)
		if _, err = a.db.ExecContext(r.Context(), `INSERT INTO system_settings(key,value,updated_at) VALUES('backupPasswordCipher',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, ciphertext, now); err != nil {
			respondError(w, http.StatusInternalServerError, "无法保存备份密码")
			return
		}
		password = req.Password
	}
	a.backupJob = &backupJob{Status: "running", StartedAt: a.now().UTC()}
	a.backupMu.Unlock()
	locked = false
	sendTelegram, uploadGoogleDrive := req.SendTelegram, req.UploadGoogleDrive
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		path, err := a.createDisasterBackup(ctx, password)
		password = ""
		if err == nil {
			var deliveryErrors []error
			if uploadGoogleDrive {
				if driveErr := a.uploadBackupToGoogleDrive(ctx, path); driveErr != nil {
					deliveryErrors = append(deliveryErrors, fmt.Errorf("google drive: %w", driveErr))
				}
			}
			if sendTelegram {
				if telegramErr := a.sendBackupToTelegram(ctx, path); telegramErr != nil {
					deliveryErrors = append(deliveryErrors, fmt.Errorf("telegram: %w", telegramErr))
				}
			}
			err = errors.Join(deliveryErrors...)
		}
		a.backupMu.Lock()
		if err != nil {
			a.backupJob.Status = "failed"
			a.backupJob.Error = "本地备份或所选推送未全部完成，请查看服务日志"
			a.log.Error("create disaster backup", "error", err)
		} else {
			a.backupJob.Status = "success"
		}
		a.backupMu.Unlock()
	}()
	respondJSON(w, http.StatusAccepted, map[string]any{"ok": true, "message": "备份任务已开始"})
}

func (a *App) handleUpdateBackupSettings(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	var req updateBackupScheduleRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if req.Days < 1 || req.Days > 365 {
		badRequest(w, errors.New("备份周期必须为 1 至 365 天"))
		return
	}
	var ciphertext string
	_ = a.db.QueryRowContext(r.Context(), `SELECT value FROM system_settings WHERE key='backupPasswordCipher'`).Scan(&ciphertext)
	if req.Password != "" {
		if !validBackupPassword(req.Password) {
			badRequest(w, errors.New("备份密码至少需要 8 个字符"))
			return
		}
		if req.Password != req.ConfirmPassword {
			badRequest(w, errors.New("两次输入的备份密码不一致"))
			return
		}
		var err error
		ciphertext, err = a.encryptBackupPassword(req.Password)
		if err != nil {
			respondError(w, 500, "无法安全保存备份密码")
			return
		}
	}
	if req.Enabled && ciphertext == "" {
		badRequest(w, errors.New("启用定时备份前请设置备份密码"))
		return
	}
	if req.Enabled && strings.TrimSpace(a.config().UpdateServiceToken) == "" {
		badRequest(w, errors.New("当前部署缺少备份密码加密密钥，请先更新部署配置"))
		return
	}
	chatID := strings.TrimSpace(req.ChatID)
	if chatID != "" && !validTelegramPrivateChatID(chatID) {
		badRequest(w, errors.New("备份 Telegram Chat ID 无效"))
		return
	}
	telegramMode := strings.TrimSpace(req.TelegramMode)
	if telegramMode != "custom" {
		telegramMode = "system"
	}
	if telegramMode == "custom" && !validTelegramPrivateChatID(chatID) {
		badRequest(w, errors.New("请选择备份群组并填写有效的 Chat ID"))
		return
	}
	if req.Enabled && req.TelegramEnabled {
		cfg := a.config()
		destination := cfg.TelegramPrivateChatID
		if telegramMode == "custom" {
			destination = chatID
		}
		if cfg.TelegramBotToken == "" || !validTelegramPrivateChatID(destination) {
			badRequest(w, errors.New("启用定时推送前请先在系统设置绑定 Telegram 机器人并配置接收位置"))
			return
		}
	}
	if req.Enabled && req.GoogleDriveEnabled && !a.loadGoogleDriveStatus(r.Context()).Connected {
		badRequest(w, errors.New("启用 Google 云端硬盘备份前请先完成授权"))
		return
	}
	secretCipher := ""
	_ = a.db.QueryRowContext(r.Context(), `SELECT value FROM system_settings WHERE key='backupGoogleClientSecretCipher'`).Scan(&secretCipher)
	if strings.TrimSpace(req.GoogleClientSecret) != "" {
		var err error
		secretCipher, err = a.encryptBackupPassword(strings.TrimSpace(req.GoogleClientSecret))
		if err != nil {
			respondError(w, 500, "无法安全保存 Google 客户端密钥")
			return
		}
	}
	folderName := strings.TrimSpace(req.GoogleFolderName)
	if folderName == "" {
		folderName = "NewSzxcn Backups"
	}
	values := map[string]string{
		"backupScheduleEnabled": fmt.Sprint(req.Enabled), "backupScheduleDays": fmt.Sprint(req.Days),
		"backupServerIp": "", "backupTelegramChatId": chatID,
		"backupTelegramMode":   telegramMode,
		"backupPasswordCipher": ciphertext, "backupTelegramEnabled": fmt.Sprint(req.TelegramEnabled),
		"backupGoogleDriveEnabled": fmt.Sprint(req.GoogleDriveEnabled), "backupGoogleClientId": strings.TrimSpace(req.GoogleClientID),
		"backupGoogleClientSecretCipher": secretCipher, "backupGoogleFolderName": folderName,
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, 500, "保存失败")
		return
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO system_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, now); err != nil {
			respondError(w, 500, "保存失败")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		respondError(w, 500, "保存失败")
		return
	}
	passwordHint := ""
	if password, err := a.decryptBackupPassword(ciphertext); err == nil {
		passwordHint = backupPasswordHint(password)
	}
	respondJSON(w, 200, backupSchedule{Enabled: req.Enabled, Days: req.Days, PasswordSet: ciphertext != "", PasswordHint: passwordHint, ServerIP: detectPublicServerIP(r.Context(), a.config().PublicHostname), ChatID: chatID, TelegramMode: telegramMode, TelegramEnabled: req.TelegramEnabled, GoogleDriveEnabled: req.GoogleDriveEnabled})
}

func (a *App) handleUpdateBackupPassword(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	var req updateBackupPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if !validBackupPassword(req.Password) {
		badRequest(w, errors.New("备份密码至少需要 8 个字符"))
		return
	}
	if req.Password != req.ConfirmPassword {
		badRequest(w, errors.New("两次输入的备份密码不一致"))
		return
	}
	ciphertext, err := a.encryptBackupPassword(req.Password)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "无法安全保存备份密码")
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err = a.db.ExecContext(r.Context(), `INSERT INTO system_settings(key,value,updated_at) VALUES('backupPasswordCipher',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, ciphertext, now); err != nil {
		respondError(w, http.StatusInternalServerError, "无法保存备份密码")
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"passwordSet": true, "passwordHint": backupPasswordHint(req.Password)})
}

func validBackupPassword(password string) bool {
	return len(password) >= 8 && len(password) <= 1024 && !strings.ContainsAny(password, "\r\n\x00")
}

func backupPasswordHint(password string) string {
	runes := []rune(password)
	if len(runes) < 2 {
		return ""
	}
	return string(runes[0]) + strings.Repeat("•", minimumInt(len(runes)-2, 10)) + string(runes[len(runes)-1])
}

func (a *App) savedBackupPassword(ctx context.Context) (string, error) {
	var ciphertext string
	if err := a.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key='backupPasswordCipher'`).Scan(&ciphertext); err != nil {
		return "", err
	}
	if strings.TrimSpace(ciphertext) == "" {
		return "", sql.ErrNoRows
	}
	return a.decryptBackupPassword(ciphertext)
}

func (a *App) handleTestBackupTelegram(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	var req testBackupTelegramRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	cfg := a.config()
	token, chatID := strings.TrimSpace(cfg.TelegramBotToken), strings.TrimSpace(cfg.TelegramPrivateChatID)
	if req.Mode == "custom" {
		chatID = strings.TrimSpace(req.ChatID)
	}
	if token == "" || !validTelegramPrivateChatID(chatID) {
		badRequest(w, errors.New("请先完成 Telegram 机器人和 Chat ID 配置"))
		return
	}
	now := a.now().Local().Format("2006-01-02 15:04:05 MST")
	message := "<b>NewSzxcn 备份通知测试</b>\n\nTelegram 备份接收配置正常。\n\n<b>测试时间：</b>" + htmlEscape(now)
	if err := a.sendTelegramMessage(r.Context(), token, chatID, message); err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleDiscoverBackupTelegramGroup(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	var req telegramCredentialsRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	token := strings.TrimSpace(a.config().TelegramBotToken)
	code := strings.ToUpper(strings.TrimSpace(req.PairingCode))
	if token == "" || code == "" {
		badRequest(w, errors.New("请先生成 Telegram 群组查询码"))
		return
	}
	a.telegramPairMu.Lock()
	pairing, ok := a.telegramPairs[code]
	a.telegramPairMu.Unlock()
	if !ok || !pairing.ExpiresAt.After(a.now().UTC()) || pairing.TokenFingerprint != telegramTokenFingerprint(token) {
		badRequest(w, errors.New("Telegram 群组查询码无效或已过期，请重新生成"))
		return
	}
	groups, err := a.discoverTelegramGroups(r.Context(), token, code)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.telegramPairMu.Lock()
	delete(a.telegramPairs, code)
	a.telegramPairMu.Unlock()
	respondJSON(w, http.StatusOK, map[string]any{"items": groups})
}

func (a *App) googleDriveOAuthConfig(ctx context.Context) (*oauth2.Config, error) {
	values := map[string]string{}
	rows, err := a.db.QueryContext(ctx, `SELECT key,value FROM system_settings WHERE key IN ('backupGoogleClientId','backupGoogleClientSecretCipher')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	secret := ""
	if values["backupGoogleClientSecretCipher"] != "" {
		secret, err = a.decryptBackupPassword(values["backupGoogleClientSecretCipher"])
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(values["backupGoogleClientId"]) == "" || secret == "" {
		return nil, errors.New("请先填写并保存 Google OAuth 客户端 ID 和密钥")
	}
	return &oauth2.Config{ClientID: values["backupGoogleClientId"], ClientSecret: secret, RedirectURL: strings.TrimRight(a.config().PublicBaseURL, "/") + "/api/admin/backups/google-drive/callback", Scopes: []string{"https://www.googleapis.com/auth/drive.file"}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"}}, nil
}

func (a *App) handleGoogleDriveConnect(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	conf, err := a.googleDriveOAuthConfig(r.Context())
	if err != nil {
		badRequest(w, err)
		return
	}
	raw, _ := json.Marshal(googleDriveOAuthState{ExpiresAt: a.now().Add(10 * time.Minute).Unix(), Nonce: newID("drive")})
	state, err := a.encryptBackupPassword(string(raw))
	if err != nil {
		respondError(w, 500, "无法创建授权请求")
		return
	}
	state = base64.RawURLEncoding.EncodeToString([]byte(state))
	respondJSON(w, 200, map[string]string{"url": conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)})
}

func (a *App) handleGoogleDriveCallback(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	encoded, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("state"))
	if err != nil {
		badRequest(w, errors.New("Google 授权状态无效"))
		return
	}
	plain, err := a.decryptBackupPassword(string(encoded))
	if err != nil {
		badRequest(w, errors.New("Google 授权状态无效"))
		return
	}
	var state googleDriveOAuthState
	if json.Unmarshal([]byte(plain), &state) != nil || state.ExpiresAt < a.now().Unix() {
		badRequest(w, errors.New("Google 授权已过期，请重新连接"))
		return
	}
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		http.Redirect(w, r, strings.TrimRight(a.config().PublicBaseURL, "/")+"/admin?section=backups&drive=error", http.StatusFound)
		return
	}
	conf, err := a.googleDriveOAuthConfig(r.Context())
	if err != nil {
		badRequest(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	token, err := conf.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		respondError(w, 400, "Google 授权交换失败")
		return
	}
	raw, _ := json.Marshal(googleDriveToken{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, Expiry: token.Expiry})
	ciphertext, err := a.encryptBackupPassword(string(raw))
	if err != nil {
		respondError(w, 500, "无法保存 Google 授权")
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO system_settings(key,value,updated_at) VALUES('backupGoogleTokenCipher',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, ciphertext, now)
	if err != nil {
		respondError(w, 500, "无法保存 Google 授权")
		return
	}
	http.Redirect(w, r, strings.TrimRight(a.config().PublicBaseURL, "/")+"/admin?section=backups&drive=connected", http.StatusFound)
}

func (a *App) handleGoogleDriveDisconnect(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	_, err := a.db.ExecContext(r.Context(), `DELETE FROM system_settings WHERE key IN ('backupGoogleTokenCipher','backupGoogleDriveEnabled')`)
	if err != nil {
		respondError(w, 500, "断开失败")
		return
	}
	respondJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) loadGoogleDriveStatus(ctx context.Context) googleDriveStatus {
	status := googleDriveStatus{FolderName: "NewSzxcn Backups"}
	rows, err := a.db.QueryContext(ctx, `SELECT key,value FROM system_settings WHERE key IN ('backupGoogleClientId','backupGoogleClientSecretCipher','backupGoogleTokenCipher','backupGoogleFolderName')`)
	if err != nil {
		return status
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) != nil {
			continue
		}
		switch key {
		case "backupGoogleClientId":
			status.ClientID = value
		case "backupGoogleClientSecretCipher":
			status.ClientSecretSet = value != ""
		case "backupGoogleTokenCipher":
			status.Connected = value != ""
		case "backupGoogleFolderName":
			if strings.TrimSpace(value) != "" {
				status.FolderName = value
			}
		}
	}
	return status
}

func (a *App) createDisasterBackup(ctx context.Context, password string) (string, error) {
	cfg := a.config()
	if !a.backupAssetsAvailable() {
		return "", errors.New("backup directories are not configured")
	}
	if err := os.MkdirAll(cfg.BackupDir, 0o700); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(cfg.BackupDir, ".staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	root := filepath.Join(work, "newszxcn-backup")
	for _, dir := range []string{"data", "mail", "dkim", "certs"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			return "", err
		}
	}
	quoted := strings.ReplaceAll(filepath.Join(root, "data", "lanqin.db"), "'", "''")
	if _, err := a.db.ExecContext(ctx, "VACUUM INTO '"+quoted+"'"); err != nil {
		return "", err
	}
	if err := copyTree(cfg.DataDir, filepath.Join(root, "data"), map[string]bool{"lanqin.db": true, "lanqin.db-wal": true, "lanqin.db-shm": true, "backups": true, "disaster-backups": true}); err != nil {
		return "", err
	}
	if cfg.MaildirRoot != "" {
		if err := copyTree(cfg.MaildirRoot, filepath.Join(root, "mail"), nil); err != nil {
			return "", err
		}
	}
	for _, item := range []struct{ src, dst string }{{"/var/lib/rspamd/dkim", "dkim"}, {"/certs", "certs"}} {
		if err := copyTree(item.src, filepath.Join(root, item.dst), nil); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := copyFile(filepath.Join(cfg.BackupSourceDir, "docker-compose.yml"), filepath.Join(root, "docker-compose.yml")); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(cfg.BackupSourceDir, ".env"), filepath.Join(root, ".env")); err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err := writeRuntimeBackupEnv(filepath.Join(root, ".env")); err != nil {
			return "", err
		}
	}
	manifest := map[string]any{"format": 1, "version": cfg.AppVersion, "createdAt": a.now().UTC(), "hostname": cfg.PublicHostname}
	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0o600); err != nil {
		return "", err
	}
	tarPath := filepath.Join(work, "backup.tar")
	if err := writeTar(tarPath, root); err != nil {
		return "", err
	}
	zstPath := tarPath + ".zst"
	if output, err := exec.CommandContext(ctx, "zstd", "-q", "-T0", "-10", tarPath, "-o", zstPath).CombinedOutput(); err != nil {
		return "", fmt.Errorf("zstd: %w: %s", err, output)
	}
	name := fmt.Sprintf("newszxcn-backup-%s-%s.tar.zst.enc", a.now().UTC().Format("20060102-150405"), strings.TrimPrefix(cfg.AppVersion, "v"))
	outPath := filepath.Join(cfg.BackupDir, name)
	cmd := exec.CommandContext(ctx, "openssl", "enc", "-aes-256-cbc", "-salt", "-pbkdf2", "-iter", "200000", "-md", "sha256", "-in", zstPath, "-out", outPath, "-pass", "stdin")
	cmd.Stdin = strings.NewReader(password)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.Remove(outPath)
		return "", fmt.Errorf("openssl: %w: %s", err, output)
	}
	if err := os.Chmod(outPath, 0o600); err != nil {
		_ = os.Remove(outPath)
		return "", err
	}
	sum, err := fileSHA256(outPath)
	if err != nil {
		_ = os.Remove(outPath)
		return "", err
	}
	if err := os.WriteFile(outPath+".sha256", []byte(sum+"  "+name+"\n"), 0o600); err != nil {
		_ = os.Remove(outPath)
		return "", err
	}
	if err := a.pruneDisasterBackups(10); err != nil {
		a.log.Warn("prune disaster backups", "error", err)
	}
	return outPath, nil
}

func (a *App) backupAssetsAvailable() bool {
	cfg := a.config()
	if strings.TrimSpace(cfg.BackupDir) == "" || strings.TrimSpace(cfg.BackupSourceDir) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(cfg.BackupSourceDir, "docker-compose.yml"))
	return err == nil && info.Mode().IsRegular()
}

func writeRuntimeBackupEnv(path string) error {
	values := make([]string, 0)
	containerOnly := map[string]bool{
		"LANQIN_BACKUP_DIR":           true,
		"LANQIN_BACKUP_SOURCE_DIR":    true,
		"LANQIN_UPDATE_SERVICE_TOKEN": true,
		"LANQIN_UPDATE_SERVICE_URL":   true,
	}
	for _, item := range os.Environ() {
		key, value, found := strings.Cut(item, "=")
		if !found || containerOnly[key] || (!strings.HasPrefix(key, "LANQIN_") && key != "TZ") {
			continue
		}
		value = strings.ReplaceAll(value, "\\", "\\\\")
		value = strings.ReplaceAll(value, "'", "\\'")
		value = strings.ReplaceAll(value, "\r", "\\r")
		value = strings.ReplaceAll(value, "\n", "\\n")
		values = append(values, key+"='"+value+"'")
	}
	sort.Strings(values)
	return os.WriteFile(path, []byte(strings.Join(values, "\n")+"\n"), 0o600)
}

func (a *App) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	path, ok := a.backupPath(chi.URLParam(r, "name"))
	if !ok {
		respondError(w, http.StatusNotFound, "备份不存在")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

func (a *App) handleVerifyBackup(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	path, ok := a.backupPath(chi.URLParam(r, "name"))
	if !ok {
		respondError(w, http.StatusNotFound, "备份不存在")
		return
	}
	actual, err := fileSHA256(path)
	if err != nil {
		respondError(w, 500, "校验失败")
		return
	}
	expectedRaw, err := os.ReadFile(path + ".sha256")
	if err != nil {
		respondError(w, 500, "校验文件缺失")
		return
	}
	expected := strings.Fields(string(expectedRaw))
	valid := len(expected) > 0 && expected[0] == actual
	respondJSON(w, 200, map[string]any{"ok": valid, "sha256": actual})
}

func (a *App) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	path, ok := a.backupPath(chi.URLParam(r, "name"))
	if !ok {
		respondError(w, 404, "备份不存在")
		return
	}
	if err := os.Remove(path); err != nil {
		respondError(w, 500, "删除失败")
		return
	}
	_ = os.Remove(path + ".sha256")
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleSendBackupTelegram(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	path, ok := a.backupPath(chi.URLParam(r, "name"))
	if !ok {
		respondError(w, 404, "备份不存在")
		return
	}
	if err := a.sendBackupToTelegram(r.Context(), path); err != nil {
		a.log.Error("send backup telegram", "error", err)
		respondError(w, 502, err.Error())
		return
	}
	respondJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) handleSendBackupGoogleDrive(w http.ResponseWriter, r *http.Request) {
	if !a.requireSystemAdmin(w, r) {
		return
	}
	path, ok := a.backupPath(chi.URLParam(r, "name"))
	if !ok {
		respondError(w, 404, "备份不存在")
		return
	}
	if err := a.uploadBackupToGoogleDrive(r.Context(), path); err != nil {
		a.log.Error("upload backup to google drive", "error", err)
		respondError(w, 502, "上传 Google 云端硬盘失败")
		return
	}
	respondJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) googleDriveClient(ctx context.Context) (*http.Client, error) {
	conf, err := a.googleDriveOAuthConfig(ctx)
	if err != nil {
		return nil, err
	}
	var ciphertext string
	if err := a.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key='backupGoogleTokenCipher'`).Scan(&ciphertext); err != nil {
		return nil, errors.New("Google 云端硬盘尚未连接")
	}
	plain, err := a.decryptBackupPassword(ciphertext)
	if err != nil {
		return nil, err
	}
	var saved googleDriveToken
	if err := json.Unmarshal([]byte(plain), &saved); err != nil {
		return nil, err
	}
	original := &oauth2.Token{AccessToken: saved.AccessToken, RefreshToken: saved.RefreshToken, Expiry: saved.Expiry, TokenType: "Bearer"}
	refreshed, err := conf.TokenSource(ctx, original).Token()
	if err != nil {
		return nil, err
	}
	if refreshed.AccessToken != original.AccessToken || !refreshed.Expiry.Equal(original.Expiry) {
		refreshToken := refreshed.RefreshToken
		if refreshToken == "" {
			refreshToken = saved.RefreshToken
		}
		raw, _ := json.Marshal(googleDriveToken{AccessToken: refreshed.AccessToken, RefreshToken: refreshToken, Expiry: refreshed.Expiry})
		ciphertext, encryptErr := a.encryptBackupPassword(string(raw))
		if encryptErr != nil {
			return nil, encryptErr
		}
		now := a.now().UTC().Format(time.RFC3339Nano)
		if _, err := a.db.ExecContext(ctx, `INSERT INTO system_settings(key,value,updated_at) VALUES('backupGoogleTokenCipher',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, ciphertext, now); err != nil {
			return nil, err
		}
	}
	return oauth2.NewClient(ctx, oauth2.StaticTokenSource(refreshed)), nil
}

func (a *App) googleDriveFolderID(ctx context.Context, client *http.Client, name string) (string, error) {
	escaped := strings.ReplaceAll(name, "'", "\\'")
	query := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", escaped)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/drive/v3/files?spaces=drive&fields=files(id,name)&pageSize=1&q="+url.QueryEscape(query), nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("drive folder lookup %s: %s", resp.Status, raw)
	}
	var list struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", err
	}
	if len(list.Files) > 0 {
		return list.Files[0].ID, nil
	}
	body, _ := json.Marshal(map[string]any{"name": name, "mimeType": "application/vnd.google-apps.folder"})
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, "https://www.googleapis.com/drive/v3/files?fields=id", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("drive folder create %s: %s", resp.Status, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", errors.New("Google 云端硬盘未返回文件夹 ID")
	}
	return created.ID, nil
}

func (a *App) uploadBackupToGoogleDrive(ctx context.Context, path string) error {
	client, err := a.googleDriveClient(ctx)
	if err != nil {
		return err
	}
	folder := a.loadGoogleDriveStatus(ctx).FolderName
	folderID, err := a.googleDriveFolderID(ctx, client, folder)
	if err != nil {
		return err
	}
	req, err := newGoogleDriveUploadRequest(ctx, path, folderID)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("drive upload %s: %s", resp.Status, raw)
	}
	return nil
}

func newGoogleDriveUploadRequest(ctx context.Context, path, folderID string) (*http.Request, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	reader, writerSide := io.Pipe()
	mw := multipart.NewWriter(writerSide)
	go func() {
		var writeErr error
		defer func() { _ = file.Close(); _ = mw.Close(); _ = writerSide.CloseWithError(writeErr) }()
		head := textproto.MIMEHeader{}
		head.Set("Content-Type", "application/json; charset=UTF-8")
		part, err := mw.CreatePart(head)
		if err != nil {
			writeErr = err
			return
		}
		metadata, _ := json.Marshal(map[string]any{"name": filepath.Base(path), "parents": []string{folderID}})
		if _, err = part.Write(metadata); err != nil {
			writeErr = err
			return
		}
		head = textproto.MIMEHeader{}
		head.Set("Content-Type", "application/octet-stream")
		part, err = mw.CreatePart(head)
		if err != nil {
			writeErr = err
			return
		}
		_, writeErr = io.Copy(part, file)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart&fields=id,name", reader)
	if err != nil {
		_ = file.Close()
		_ = writerSide.CloseWithError(err)
		return nil, err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())
	return req, nil
}

func (a *App) sendBackupToTelegram(ctx context.Context, path string) error {
	schedule, _ := a.loadBackupSchedule(ctx)
	token, chatID, err := a.backupTelegramCredentials(ctx, schedule)
	if err != nil {
		return err
	}
	if token == "" || !validTelegramPrivateChatID(chatID) {
		return errors.New("请先在系统设置绑定 Telegram 机器人")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > backupTelegramLimit {
		return errors.New("备份超过 Telegram 发送上限，请下载后保存到其他存储")
	}
	report, err := a.backupTelegramReport(ctx, path, info)
	if err != nil {
		return err
	}
	if err := a.sendTelegramMessage(ctx, token, chatID, report); err != nil {
		return err
	}
	return a.sendTelegramDocument(ctx, token, chatID, path)
}

func (a *App) backupTelegramCredentials(ctx context.Context, schedule backupSchedule) (string, string, error) {
	cfg := a.config()
	if schedule.TelegramMode != "custom" {
		return strings.TrimSpace(cfg.TelegramBotToken), strings.TrimSpace(cfg.TelegramPrivateChatID), nil
	}
	return strings.TrimSpace(cfg.TelegramBotToken), strings.TrimSpace(schedule.ChatID), nil
}

func (a *App) backupTelegramReport(ctx context.Context, path string, info os.FileInfo) (string, error) {
	cfg := a.config()
	sum, _ := fileSHA256(path)
	domains, err := queryBackupStrings(ctx, a.db, `SELECT name FROM domains ORDER BY name`)
	if err != nil {
		return "", err
	}
	admins, err := queryBackupStrings(ctx, a.db, `SELECT email FROM users WHERE role='admin' ORDER BY email`)
	if err != nil {
		return "", err
	}
	users, err := queryBackupStrings(ctx, a.db, `SELECT email FROM users WHERE role='user' ORDER BY email`)
	if err != nil {
		return "", err
	}
	mailboxes, err := queryBackupStrings(ctx, a.db, `SELECT address FROM mailboxes ORDER BY address`)
	if err != nil {
		return "", err
	}
	list := func(items []string) string {
		if len(items) == 0 {
			return "无"
		}
		total, suffix := len(items), ""
		if total > 10 {
			items = items[:10]
			suffix = fmt.Sprintf(" 等 %d 个", total)
		}
		for i := range items {
			value, truncated := truncateRunes(items[i], 80)
			if truncated {
				value += "..."
			}
			items[i] = htmlEscape(value)
		}
		return strings.Join(items, "、") + suffix
	}
	serverIP := detectPublicServerIP(ctx, cfg.PublicHostname)
	if serverIP == "" {
		serverIP = "未检测到"
	}
	return fmt.Sprintf("<b>%s 备份成功</b>\n\n<b>邮局域名：</b>%s\n<b>服务器 IP：</b>%s\n<b>系统版本：</b>%s\n\n<b>已有域名：</b>\n%s\n\n<b>管理员账号：</b>\n%s\n\n<b>普通用户账号：</b>\n%s\n\n<b>邮箱账号：</b>\n%s\n\n<b>备份文件：</b>%s\n<b>文件大小：</b>%s\n<b>SHA-256：</b><code>%s</code>\n\n<b>恢复教程：</b>\n1. 请不要解压、改名或修改压缩备份文件。\n2. 将原始附件上传到新服务器的 <code>/root/</code> 目录。\n3. 运行官方安装脚本，显示管理菜单后输入 2，选择“备份恢复”。\n4. 选择“本地上传”，系统会自动检测 /root/ 中的备份。\n5. 只有一份时自动选中；多份时显示 1、2、3 等序号。\n6. 输入对应序号，例如输入 1 恢复第 1 份。\n7. 输入备份密码后开始恢复。没有检测到文件时才手动输入路径。\n8. 恢复完成后，账号继续使用原登录密码。\n9. 以后需要管理系统时，可以直接输入 ns 打开管理菜单。\n\n<b>安全提示：</b>备份密码不会发送到 Telegram，请从 1Password 等独立位置取用。", info.ModTime().Local().Format("2006-01-02"), htmlEscape(cfg.PublicHostname), htmlEscape(serverIP), htmlEscape(cfg.AppVersion), list(domains), list(admins), list(users), list(mailboxes), htmlEscape(filepath.Base(path)), humanBackupBytes(info.Size()), sum), nil
}

func queryBackupStrings(ctx context.Context, db *sql.DB, query string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func humanBackupBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	size := float64(value)
	unit := "B"
	for _, next := range units {
		size /= 1024
		unit = next
		if size < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", size, unit)
}

func (a *App) sendTelegramDocument(ctx context.Context, token, chatID, path string) error {
	reader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	go func() {
		var writeErr error
		defer func() { _ = writer.Close(); _ = pipeWriter.CloseWithError(writeErr) }()
		if writeErr = writer.WriteField("chat_id", chatID); writeErr != nil {
			return
		}
		if writeErr = writer.WriteField("caption", "NewSzxcn 加密备份\n请将备份密码单独保管，不要发送到同一聊天。"); writeErr != nil {
			return
		}
		var part io.Writer
		part, writeErr = writer.CreateFormFile("document", filepath.Base(path))
		if writeErr != nil {
			return
		}
		var file *os.File
		file, writeErr = os.Open(path)
		if writeErr != nil {
			return
		}
		defer file.Close()
		_, writeErr = io.Copy(part, file)
	}()
	endpoint := strings.TrimRight(a.telegramURL, "/") + "/bot" + token + "/sendDocument"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("telegram %s: %s", resp.Status, raw)
	}
	return nil
}

func (a *App) listBackups() ([]backupItem, error) {
	dir := a.config().BackupDir
	if dir == "" {
		return []backupItem{}, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []backupItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]backupItem, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.zst.enc") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		item := backupItem{Name: entry.Name(), Size: info.Size(), CreatedAt: info.ModTime().UTC()}
		if raw, err := os.ReadFile(filepath.Join(dir, entry.Name()+".sha256")); err == nil {
			fields := strings.Fields(string(raw))
			if len(fields) > 0 {
				item.SHA256 = fields[0]
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (a *App) pruneDisasterBackups(keep int) error {
	items, err := a.listBackups()
	if err != nil {
		return err
	}
	for _, item := range items[minimumInt(keep, len(items)):] {
		path, ok := a.backupPath(item.Name)
		if !ok {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		_ = os.Remove(path + ".sha256")
	}
	return nil
}

func minimumInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (a *App) loadBackupSchedule(ctx context.Context) (backupSchedule, error) {
	result := backupSchedule{Days: 7, TelegramMode: "system", TelegramEnabled: true}
	rows, err := a.db.QueryContext(ctx, `SELECT key,value FROM system_settings WHERE key IN ('backupScheduleEnabled','backupScheduleDays','backupServerIp','backupTelegramChatId','backupTelegramMode','backupPasswordCipher','backupTelegramEnabled','backupGoogleDriveEnabled')`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return result, err
		}
		switch key {
		case "backupScheduleEnabled":
			result.Enabled = value == "true"
		case "backupScheduleDays":
			if _, err := fmt.Sscan(value, &result.Days); err != nil || result.Days < 1 {
				result.Days = 7
			}
		case "backupServerIp":
			result.ServerIP = value
		case "backupTelegramChatId":
			result.ChatID = value
		case "backupTelegramMode":
			if value == "custom" {
				result.TelegramMode = "custom"
			}
		case "backupPasswordCipher":
			result.PasswordSet = value != ""
			if value != "" {
				if password, err := a.decryptBackupPassword(value); err == nil {
					result.PasswordHint = backupPasswordHint(password)
				}
			}
		case "backupTelegramEnabled":
			result.TelegramEnabled = value == "true"
		case "backupGoogleDriveEnabled":
			result.GoogleDriveEnabled = value == "true"
		}
	}
	return result, rows.Err()
}

func (a *App) backupScheduleWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	a.runScheduledBackup(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runScheduledBackup(ctx)
		}
	}
}

func (a *App) runScheduledBackup(ctx context.Context) {
	schedule, err := a.loadBackupSchedule(ctx)
	if err != nil || !schedule.Enabled {
		return
	}
	var last string
	_ = a.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key='backupScheduleLastRun'`).Scan(&last)
	if parsed, err := time.Parse(time.RFC3339Nano, last); err == nil && a.now().UTC().Before(parsed.Add(time.Duration(schedule.Days)*24*time.Hour)) {
		return
	}
	var ciphertext string
	if err := a.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key='backupPasswordCipher'`).Scan(&ciphertext); err != nil {
		return
	}
	password, err := a.decryptBackupPassword(ciphertext)
	if err != nil {
		a.log.Error("decrypt scheduled backup password", "error", err)
		return
	}
	a.backupMu.Lock()
	if a.backupJob != nil && a.backupJob.Status == "running" {
		a.backupMu.Unlock()
		return
	}
	a.backupJob = &backupJob{Status: "running", StartedAt: a.now().UTC()}
	a.backupMu.Unlock()
	path, runErr := a.createDisasterBackup(ctx, password)
	password = ""
	localBackupSucceeded := runErr == nil
	if localBackupSucceeded {
		now := a.now().UTC().Format(time.RFC3339Nano)
		_, _ = a.db.ExecContext(ctx, `INSERT INTO system_settings(key,value,updated_at) VALUES('backupScheduleLastRun',?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, now, now)
		var deliveryErrors []error
		if schedule.GoogleDriveEnabled {
			if driveErr := a.uploadBackupToGoogleDrive(ctx, path); driveErr != nil {
				deliveryErrors = append(deliveryErrors, fmt.Errorf("google drive: %w", driveErr))
			}
		}
		if schedule.TelegramEnabled {
			if telegramErr := a.sendBackupToTelegram(ctx, path); telegramErr != nil {
				deliveryErrors = append(deliveryErrors, fmt.Errorf("telegram: %w", telegramErr))
			}
		}
		runErr = errors.Join(deliveryErrors...)
	}
	status := "success"
	publicError := ""
	if runErr != nil {
		status = "failed"
		publicError = "定时备份或云端推送失败，请查看服务日志"
		a.log.Error("scheduled backup", "error", runErr)
	}
	a.backupMu.Lock()
	a.backupJob.Status, a.backupJob.Error = status, publicError
	a.backupMu.Unlock()
}

func (a *App) backupEncryptionKey() []byte {
	cfg := a.config()
	secret := strings.TrimSpace(cfg.UpdateServiceToken)
	if secret == "" {
		secret = strings.TrimSpace(cfg.ExternalIMAPSecretKey)
	}
	sum := sha256.Sum256([]byte("newszxcn-backup-schedule:" + secret))
	return sum[:]
}

func (a *App) encryptBackupPassword(password string) (string, error) {
	if strings.TrimSpace(a.config().UpdateServiceToken) == "" && strings.TrimSpace(a.config().ExternalIMAPSecretKey) == "" {
		return "", errors.New("backup encryption key is not configured")
	}
	block, err := aes.NewCipher(a.backupEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, []byte(password), nil)...)), nil
}

func (a *App) decryptBackupPassword(value string) (string, error) {
	if strings.TrimSpace(a.config().UpdateServiceToken) == "" && strings.TrimSpace(a.config().ExternalIMAPSecretKey) == "" {
		return "", errors.New("backup encryption key is not configured")
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(a.backupEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid backup password ciphertext")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (a *App) backupPath(name string) (string, bool) {
	if filepath.Base(name) != name || !strings.HasPrefix(name, "newszxcn-backup-") || !strings.HasSuffix(name, ".tar.zst.enc") {
		return "", false
	}
	path := filepath.Join(a.config().BackupDir, name)
	info, err := os.Stat(path)
	return path, err == nil && !info.IsDir()
}

func copyTree(src, dst string, skip map[string]bool) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		first := strings.Split(rel, string(os.PathSeparator))[0]
		if skip[first] {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return closeErr
}

func writeTar(path, root string) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(out)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(filepath.Dir(root), path)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			return err
		}
		return nil
	})
	closeErr := tw.Close()
	fileCloseErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fileCloseErr
}
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
