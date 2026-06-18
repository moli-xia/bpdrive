package app

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	authorizeURL = "https://openapi.baidu.com/oauth/2.0/authorize"
	tokenURL     = "https://openapi.baidu.com/oauth/2.0/token"
	xpanFileURL  = "https://pan.baidu.com/rest/2.0/xpan/file"
	xpanMediaURL = "https://pan.baidu.com/rest/2.0/xpan/multimedia"
	xpanNasURL   = "https://pan.baidu.com/rest/2.0/xpan/nas"
	uploadURL    = "https://d.pcs.baidu.com/rest/2.0/pcs/superfile2"
)

type BaiduClient struct {
	http *http.Client
}

type BaiduError struct {
	Errno  int    `json:"errno"`
	Errmsg string `json:"errmsg"`
}

func NewBaiduClient() *BaiduClient {
	return &BaiduClient{http: &http.Client{Timeout: 120 * time.Second}}
}

func AuthURL(cfg Config) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", cfg.AppKey)
	v.Set("redirect_uri", cfg.RedirectURI)
	v.Set("scope", "basic,netdisk")
	v.Set("display", "tv")
	v.Set("qrcode", "1")
	v.Set("force_login", "1")
	return authorizeURL + "?" + v.Encode()
}

func (c *BaiduClient) ExchangeCode(cfg Config, code string) (Token, error) {
	v := url.Values{}
	v.Set("grant_type", "authorization_code")
	v.Set("code", strings.TrimSpace(code))
	v.Set("client_id", cfg.AppKey)
	v.Set("client_secret", cfg.SecretKey)
	v.Set("redirect_uri", cfg.RedirectURI)
	var token Token
	if err := c.getJSON(tokenURL+"?"+v.Encode(), &token); err != nil {
		return token, err
	}
	token.CreatedAt = time.Now().Unix()
	return token, nil
}

func (c *BaiduClient) Refresh(cfg Config) (Token, error) {
	if cfg.Token.RefreshToken == "" {
		return cfg.Token, errors.New("missing refresh token")
	}
	if cfg.Token.RefreshURL != "" {
		v := url.Values{}
		v.Set("refresh_token", cfg.Token.RefreshToken)
		var token Token
		if err := c.getJSON(cfg.Token.RefreshURL+"?"+v.Encode(), &token); err != nil {
			return token, err
		}
		token.CreatedAt = time.Now().Unix()
		if token.GrantURL == "" {
			token.GrantURL = cfg.Token.GrantURL
		}
		if token.RefreshURL == "" {
			token.RefreshURL = cfg.Token.RefreshURL
		}
		return token, nil
	}
	v := url.Values{}
	v.Set("grant_type", "refresh_token")
	v.Set("refresh_token", cfg.Token.RefreshToken)
	v.Set("client_id", cfg.AppKey)
	v.Set("client_secret", cfg.SecretKey)
	var token Token
	if err := c.getJSON(tokenURL+"?"+v.Encode(), &token); err != nil {
		return token, err
	}
	token.CreatedAt = time.Now().Unix()
	return token, nil
}

func (c *BaiduClient) UserInfo(accessToken string) (User, error) {
	v := url.Values{}
	v.Set("method", "uinfo")
	v.Set("access_token", accessToken)
	var raw struct {
		BaiduName   string      `json:"baidu_name"`
		NetdiskName string      `json:"netdisk_name"`
		AvatarURL   string      `json:"avatar_url"`
		UK          json.Number `json:"uk"`
		VipType     interface{} `json:"vip_type"`
		BaiduError
	}
	if err := c.getJSON(xpanNasURL+"?"+v.Encode(), &raw); err != nil {
		return User{}, err
	}
	uk, _ := raw.UK.Int64()
	return User{
		BaiduName: raw.BaiduName, NetdiskName: raw.NetdiskName, AvatarURL: raw.AvatarURL,
		UK: uk, VipType: fmt.Sprint(raw.VipType),
	}, nil
}

type FileItem struct {
	FSID        json.Number `json:"fs_id"`
	Path        string      `json:"path"`
	ServerName  string      `json:"server_filename"`
	Size        int64       `json:"size"`
	IsDir       int         `json:"isdir"`
	ServerMTime int64       `json:"server_mtime"`
	MD5         string      `json:"md5"`
	DLink       string      `json:"dlink,omitempty"`
	RelPath     string      `json:"rel_path"`
	SizeText    string      `json:"size_text"`
	MTimeText   string      `json:"mtime_text"`
}

func (c *BaiduClient) List(accessToken, dir string) ([]FileItem, error) {
	v := url.Values{}
	v.Set("method", "list")
	v.Set("access_token", accessToken)
	v.Set("dir", dir)
	v.Set("order", "name")
	v.Set("desc", "0")
	v.Set("start", "0")
	v.Set("limit", "1000")
	v.Set("web", "1")
	var raw struct {
		List []FileItem `json:"list"`
		BaiduError
	}
	if err := c.getJSON(xpanFileURL+"?"+v.Encode(), &raw); err != nil {
		return nil, err
	}
	return raw.List, nil
}

func (c *BaiduClient) Meta(accessToken string, fsid string, dlink bool) (FileItem, error) {
	v := url.Values{}
	base := xpanFileURL
	method := "metas"
	if dlink {
		base = xpanMediaURL
		method = "filemetas"
	}
	v.Set("method", method)
	v.Set("access_token", accessToken)
	v.Set("fsids", "["+fsid+"]")
	if dlink {
		v.Set("dlink", "1")
	}
	var raw struct {
		List []FileItem `json:"list"`
		BaiduError
	}
	if err := c.getJSON(base+"?"+v.Encode(), &raw); err != nil {
		return FileItem{}, err
	}
	if len(raw.List) == 0 {
		return FileItem{}, errors.New("file not found")
	}
	return raw.List[0], nil
}

func (c *BaiduClient) Mkdir(accessToken, p string) (map[string]interface{}, error) {
	return c.form(xpanFileURL, url.Values{"method": {"create"}, "access_token": {accessToken}}, url.Values{
		"path": {p}, "isdir": {"1"}, "rtype": {"1"}, "block_list": {"[]"},
	})
}

func (c *BaiduClient) FileManager(accessToken, opera string, filelist interface{}) (map[string]interface{}, error) {
	b, _ := json.Marshal(filelist)
	return c.form(xpanFileURL, url.Values{"method": {"filemanager"}, "access_token": {accessToken}, "opera": {opera}}, url.Values{
		"async": {"0"}, "ondup": {"newcopy"}, "filelist": {string(b)},
	})
}

func (c *BaiduClient) Upload(accessToken, remotePath, localFile string, rtype int) (map[string]interface{}, error) {
	size, blocks, err := md5Blocks(localFile)
	if err != nil {
		return nil, err
	}
	blockJSON, _ := json.Marshal(blocks)
	pre, err := c.form(xpanFileURL, url.Values{"method": {"precreate"}, "access_token": {accessToken}}, url.Values{
		"path": {remotePath}, "size": {strconv.FormatInt(size, 10)}, "isdir": {"0"}, "autoinit": {"1"},
		"rtype": {strconv.Itoa(rtype)}, "block_list": {string(blockJSON)},
	})
	if err != nil {
		return nil, err
	}
	uploadID, _ := pre["uploadid"].(string)
	if uploadID == "" {
		return nil, errors.New("precreate did not return uploadid")
	}
	if err := c.uploadBlocks(accessToken, localFile, remotePath, uploadID, len(blocks)); err != nil {
		return nil, err
	}
	return c.form(xpanFileURL, url.Values{"method": {"create"}, "access_token": {accessToken}}, url.Values{
		"path": {remotePath}, "size": {strconv.FormatInt(size, 10)}, "isdir": {"0"}, "rtype": {strconv.Itoa(rtype)},
		"uploadid": {uploadID}, "block_list": {string(blockJSON)},
	})
}

func (c *BaiduClient) DownloadURL(accessToken, fsid string) (string, string, error) {
	meta, err := c.Meta(accessToken, fsid, true)
	if err != nil {
		return "", "", err
	}
	if meta.DLink == "" {
		return "", "", errors.New("baidu did not return dlink")
	}
	sep := "&"
	if !strings.Contains(meta.DLink, "?") {
		sep = "?"
	}
	return meta.DLink + sep + "access_token=" + url.QueryEscape(accessToken), meta.ServerName, nil
}

func (c *BaiduClient) DownloadInfo(accessToken, fsid string) (string, string, int64, error) {
	meta, err := c.Meta(accessToken, fsid, true)
	if err != nil {
		return "", "", 0, err
	}
	if meta.DLink == "" {
		return "", "", 0, errors.New("baidu did not return dlink")
	}
	return meta.DLink, meta.ServerName, meta.Size, nil
}

func (c *BaiduClient) getJSON(u string, out interface{}) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "dpdrive/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("baidu http %d: %s", resp.StatusCode, string(b))
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("baidu json error: %s", string(b))
	}
	if err := decodeBaiduErr(b); err != nil {
		return err
	}
	return nil
}

func (c *BaiduClient) form(base string, query, form url.Values) (map[string]interface{}, error) {
	u := base + "?" + query.Encode()
	req, err := http.NewRequest("POST", u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "dpdrive/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("baidu http %d: %s", resp.StatusCode, string(b))
	}
	if err := decodeBaiduErr(b); err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("baidu json error: %s", string(b))
	}
	return out, nil
}

func decodeBaiduErr(b []byte) error {
	var e BaiduError
	if err := json.Unmarshal(b, &e); err == nil && e.Errno != 0 {
		if e.Errmsg == "" {
			e.Errmsg = string(b)
		}
		return fmt.Errorf("baidu errno %d: %s", e.Errno, e.Errmsg)
	}
	return nil
}

func md5Blocks(file string) (int64, []string, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, nil, err
	}
	buf := make([]byte, 4*1024*1024)
	var blocks []string
	for {
		n, er := io.ReadFull(f, buf)
		if er == io.ErrUnexpectedEOF || er == io.EOF {
			if n > 0 {
				sum := md5.Sum(buf[:n])
				blocks = append(blocks, hex.EncodeToString(sum[:]))
			}
			break
		}
		if er != nil {
			return 0, nil, er
		}
		sum := md5.Sum(buf[:n])
		blocks = append(blocks, hex.EncodeToString(sum[:]))
	}
	if len(blocks) == 0 {
		sum := md5.Sum(nil)
		blocks = append(blocks, hex.EncodeToString(sum[:]))
	}
	return info.Size(), blocks, nil
}

func (c *BaiduClient) uploadBlocks(accessToken, localFile, remotePath, uploadID string, blockCount int) error {
	f, err := os.Open(localFile)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 4*1024*1024)
	for i := 0; i < blockCount; i++ {
		n, er := io.ReadFull(f, buf)
		if er != nil && er != io.ErrUnexpectedEOF && er != io.EOF {
			return er
		}
		if n == 0 && i > 0 {
			break
		}
		if err := c.uploadPart(accessToken, remotePath, uploadID, i, buf[:n]); err != nil {
			return err
		}
	}
	return nil
}

func (c *BaiduClient) uploadPart(accessToken, remotePath, uploadID string, seq int, data []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "blob")
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	q := url.Values{}
	q.Set("method", "upload")
	q.Set("type", "tmpfile")
	q.Set("access_token", accessToken)
	q.Set("path", remotePath)
	q.Set("uploadid", uploadID)
	q.Set("partseq", strconv.Itoa(seq))
	req, err := http.NewRequest("POST", uploadURL+"?"+q.Encode(), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "pan.baidu.com")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("baidu upload http %d: %s", resp.StatusCode, string(b))
	}
	return decodeBaiduErr(b)
}
