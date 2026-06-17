package app

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	envBaiduAppKey    = "BPDRIVE_BAIDU_APP_KEY"
	envBaiduSecretKey = "BPDRIVE_BAIDU_SECRET_KEY"
	envBaiduRedirect  = "BPDRIVE_BAIDU_REDIRECT_URI"
)

type Config struct {
	AppKey      string `json:"app_key"`
	SecretKey   string `json:"secret_key"`
	RedirectURI string `json:"redirect_uri"`
	PHPBaseURL  string `json:"php_base_url"`
	DefaultDir  string `json:"default_dir"`
	AdminUser   string `json:"admin_user"`
	AdminPass   string `json:"admin_pass"`
	SiteTitle   string `json:"site_title"`
	Token       Token  `json:"token"`
	User        User   `json:"user"`
	UpdatedAt   int64  `json:"updated_at"`
}

type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	CreatedAt    int64  `json:"created_at"`
	GrantURL     string `json:"grant_url"`
	RefreshURL   string `json:"refresh_url"`
}

type User struct {
	BaiduName   string `json:"baidu_name"`
	NetdiskName string `json:"netdisk_name"`
	AvatarURL   string `json:"avatar_url"`
	UK          int64  `json:"uk"`
	VipType     string `json:"vip_type"`
}

type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dataDir, "config.json")}
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg = defaultConfig()
	b, err := ioutil.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s.saveLocked()
	}
	if err != nil {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &s.cfg); err != nil {
			return err
		}
	}
	normalizeConfig(&s.cfg)
	return nil
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	normalizeConfig(&s.cfg)
	s.cfg.UpdatedAt = time.Now().Unix()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	normalizeConfig(&s.cfg)
	s.cfg.UpdatedAt = time.Now().Unix()
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := ioutil.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func defaultConfig() Config {
	return Config{
		AppKey:      os.Getenv(envBaiduAppKey),
		SecretKey:   os.Getenv(envBaiduSecretKey),
		RedirectURI: getenvDefault(envBaiduRedirect, "oob"),
		DefaultDir:  "/",
		AdminUser:   "admin",
		AdminPass:   "admin",
		SiteTitle:   "度盘",
	}
}

func normalizeConfig(c *Config) {
	if c.AppKey == "" {
		c.AppKey = os.Getenv(envBaiduAppKey)
	}
	if c.SecretKey == "" {
		c.SecretKey = os.Getenv(envBaiduSecretKey)
	}
	if c.RedirectURI == "" {
		c.RedirectURI = getenvDefault(envBaiduRedirect, "oob")
	}
	if c.DefaultDir == "" {
		c.DefaultDir = "/"
	}
	c.DefaultDir = CleanPath(c.DefaultDir)
	if c.AdminUser == "" {
		c.AdminUser = "admin"
	}
	if c.AdminPass == "" || c.AdminPass == "251024" {
		c.AdminPass = "admin"
	}
	if c.SiteTitle == "" {
		c.SiteTitle = "度盘"
	}
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
