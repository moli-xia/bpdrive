package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	baiduPassportURL = "https://passport.baidu.com"
	oauthStaticURL   = "https://openapi.baidu.com/static/oauth/html/bdstoken_jump.html"
)

type BaiduQRSession struct {
	ID        string
	Sign      string
	ImageURL  string
	AuthURL   string
	Code      string
	CodeFrom  string
	Config    Config
	Client    *http.Client
	CreatedAt time.Time
	Status    string
	Message   string
}

func (s *Server) authSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.newBaiduQRSession(r)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	s.authMu.Lock()
	s.authSessions[session.ID] = session
	for id, old := range s.authSessions {
		if time.Since(old.CreatedAt) > 15*time.Minute {
			delete(s.authSessions, id)
		}
	}
	s.authMu.Unlock()

	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"id":      session.ID,
		"status":  session.Status,
		"message": session.Message,
		"image":   "/api/auth/qrcode-image?id=" + url.QueryEscape(session.ID),
	})
}

func (s *Server) authQRCodeImage(w http.ResponseWriter, r *http.Request) {
	session := s.getBaiduQRSession(r.URL.Query().Get("id"))
	if session == nil {
		fail(w, 404, "二维码会话不存在，请刷新二维码")
		return
	}
	resp, err := session.baiduGET(session.ImageURL, session.AuthURL)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fail(w, resp.StatusCode, "百度二维码图片读取失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "image/png")
	}
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) authPoll(w http.ResponseWriter, r *http.Request) {
	session := s.getBaiduQRSession(r.URL.Query().Get("id"))
	if session == nil {
		fail(w, 404, "二维码会话不存在，请刷新二维码")
		return
	}
	result, err := s.pollBaiduQRSession(session)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) getBaiduQRSession(id string) *BaiduQRSession {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	return s.authSessions[id]
}

func (s *Server) newBaiduQRSession(r *http.Request) (*BaiduQRSession, error) {
	cfg := s.store.Get()
	authURL := s.authURLForRequest(r, cfg)
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, Timeout: 45 * time.Second}
	session := &BaiduQRSession{
		ID:        randomID(),
		AuthURL:   authURL,
		Config:    cfg,
		Client:    client,
		CreatedAt: time.Now(),
		Status:    "waiting",
		Message:   "等待扫码",
	}

	// Open the OAuth page first so Baidu sets the same cookies their own TV login uses.
	resp, err := session.baiduGET(authURL, "")
	if err == nil {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}

	v := url.Values{}
	v.Set("lp", "pc")
	v.Set("qrloginfrom", "pc")
	v.Set("gid", "")
	v.Set("oauthLog", "")
	v.Set("apiver", "v3")
	v.Set("tpl", "dev")
	v.Set("logPage", "login")
	v.Set("tt", strconvNow())
	v.Set("callback", "bdqr")
	body, err := session.baiduGETText(baiduPassportURL+"/v2/api/getqrcode?"+v.Encode(), authURL)
	if err != nil {
		return nil, err
	}
	var qr struct {
		Errno  int    `json:"errno"`
		ImgURL string `json:"imgurl"`
		Sign   string `json:"sign"`
		Prompt string `json:"prompt"`
	}
	if err := parseJSONP(body, &qr); err != nil {
		return nil, err
	}
	if qr.Errno != 0 || qr.Sign == "" || qr.ImgURL == "" {
		return nil, fmt.Errorf("百度二维码创建失败: errno=%d", qr.Errno)
	}
	session.Sign = qr.Sign
	session.ImageURL = normalizeBaiduURL(qr.ImgURL)
	return session, nil
}

func (s *Server) pollBaiduQRSession(session *BaiduQRSession) (map[string]interface{}, error) {
	if session.Status == "authorized" {
		return map[string]interface{}{"ok": true, "status": "authorized", "logged_in": true, "message": "授权成功"}, nil
	}
	if time.Since(session.CreatedAt) > 10*time.Minute {
		session.Status = "expired"
		session.Message = "二维码已过期"
		return map[string]interface{}{"ok": true, "status": session.Status, "message": session.Message}, nil
	}

	v := url.Values{}
	v.Set("channel_id", session.Sign)
	v.Set("gid", "")
	v.Set("tpl", "dev")
	v.Set("_sdkFrom", "1")
	v.Set("apiver", "v3")
	v.Set("tt", strconvNow())
	v.Set("callback", "bdqrpoll")
	body, err := session.baiduGETText(baiduPassportURL+"/channel/unicast?"+v.Encode(), session.AuthURL)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Errno   interface{}     `json:"errno"`
		Channel json.RawMessage `json:"channel_v"`
	}
	if err := parseJSONP(body, &raw); err != nil {
		return nil, err
	}
	channel := map[string]interface{}{}
	if len(raw.Channel) > 0 && string(raw.Channel) != "null" {
		var encoded string
		if err := json.Unmarshal(raw.Channel, &encoded); err == nil {
			_ = json.Unmarshal([]byte(encoded), &channel)
		} else {
			_ = json.Unmarshal(raw.Channel, &channel)
		}
	}
	status := fmt.Sprint(channel["status"])
	switch status {
	case "1":
		session.Status = "scanned"
		session.Message = "已扫码，请在手机端确认"
		return map[string]interface{}{"ok": true, "status": session.Status, "message": session.Message}, nil
	case "2":
		session.Status = "expired"
		session.Message = "二维码已过期"
		return map[string]interface{}{"ok": true, "status": session.Status, "message": session.Message}, nil
	case "0":
		bduss := fmt.Sprint(channel["v"])
		if bduss == "" || bduss == "<nil>" {
			return nil, errors.New("百度扫码已确认，但未返回登录凭据")
		}
		channelURL := fmt.Sprint(channel["u"])
		if err := s.finishBaiduQRAuthorization(session, bduss, channelURL); err != nil {
			session.Status = "error"
			session.Message = err.Error()
			return map[string]interface{}{"ok": true, "status": session.Status, "message": session.Message}, nil
		}
		session.Status = "authorized"
		session.Message = "授权成功"
		return map[string]interface{}{"ok": true, "status": session.Status, "logged_in": true, "message": session.Message}, nil
	default:
		session.Status = "waiting"
		session.Message = "等待扫码"
		return map[string]interface{}{"ok": true, "status": session.Status, "message": session.Message}, nil
	}
}

func (s *Server) finishBaiduQRAuthorization(session *BaiduQRSession, bduss, channelURL string) error {
	originURL := session.continueAuthURL(channelURL)
	v := url.Values{}
	v.Set("tt", strconvNow())
	v.Set("bduss", bduss)
	v.Set("u", url.QueryEscape(originURL))
	v.Set("qrcode", "1")
	v.Set("tpl", "dev")
	v.Set("callback", "bdusslogin")
	body, err := session.baiduGETText(baiduPassportURL+"/v2/api/bdusslogin?"+v.Encode(), originURL)
	if err != nil {
		return err
	}
	var login struct {
		Data struct {
			U string `json:"u"`
		} `json:"data"`
		ErrInfo struct {
			No  interface{} `json:"no"`
			Msg string      `json:"msg"`
		} `json:"errInfo"`
	}
	if err := parseJSONP(body, &login); err != nil {
		return err
	}
	if fmt.Sprint(login.ErrInfo.No) != "0" {
		return fmt.Errorf("百度扫码登录失败: %s", login.ErrInfo.Msg)
	}
	continueURL := session.continueAuthURL(login.Data.U)
	if continueURL != "" {
		session.AuthURL = continueURL
		if resp, err := session.baiduGET(continueURL, session.AuthURL); err == nil {
			io.Copy(ioutil.Discard, resp.Body)
			resp.Body.Close()
		}
	}

	stoken, err := session.fetchOAuthSToken(session.AuthURL)
	if err != nil {
		return err
	}
	bdstoken, err := session.fetchBDSToken(stoken, session.AuthURL)
	if err != nil {
		return err
	}
	code, source, err := session.submitOAuthAuthorize(bdstoken, session.AuthURL)
	if err != nil {
		return err
	}
	session.Code = code
	session.CodeFrom = source
	token, err := s.baidu.ExchangeCode(session.Config, code)
	if err != nil {
		return fmt.Errorf("%w（code 来源：%s，长度：%d）", err, source, len(code))
	}
	user, _ := s.baidu.UserInfo(token.AccessToken)
	return s.store.Update(func(c *Config) { c.Token = token; c.User = user })
}

func (session *BaiduQRSession) fetchOAuthSToken(authURL string) (string, error) {
	v := url.Values{}
	v.Set("tpl", "dev")
	v.Set("return_type", "2")
	v.Set("callback", "logaback")
	body, err := session.baiduGETText(baiduPassportURL+"/v3/login/api/auth?"+v.Encode(), authURL)
	if err != nil {
		return "", err
	}
	var res map[string]interface{}
	if err := parseJSONP(body, &res); err != nil {
		return "", err
	}
	if stoken := fmt.Sprint(res["stoken"]); stoken != "" && stoken != "<nil>" {
		return stoken, nil
	}
	return "", fmt.Errorf("百度登录后未返回 stoken: %s", body)
}

func (session *BaiduQRSession) fetchBDSToken(stoken, authURL string) (string, error) {
	form := url.Values{}
	form.Set("jumpurl", oauthStaticURL)
	form.Set("etken", stoken)
	req, err := http.NewRequest("POST", "https://openapi.baidu.com/oauth/2.0/getbdstoken", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", authURL)
	req.Header.Set("User-Agent", baiduUserAgent)

	resp, err := session.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	body := string(bodyBytes)
	if loc := resp.Request.URL.String(); strings.Contains(loc, "bdstoken=") {
		if u, err := url.Parse(loc); err == nil {
			if token := u.Query().Get("bdstoken"); token != "" {
				return token, nil
			}
		}
	}
	if token := extractValue(body, `bdstoken["'=:\s]+([A-Za-z0-9._-]+)`); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("百度授权票据获取失败: %s", compact(body))
}

func (session *BaiduQRSession) submitOAuthAuthorize(bdstoken, authURL string) (string, string, error) {
	form := url.Values{}
	form.Set("bdstoken", bdstoken)
	form.Set("client_id", session.Config.AppKey)
	form.Set("redirect_uri", session.Config.RedirectURI)
	form.Set("response_type", "code")
	form.Set("display", "tv")
	form.Set("scope", "basic,netdisk")
	form.Set("grant_permissions", "basic,netdisk")
	form.Add("grant_permissions_arr", "basic")
	form.Add("grant_permissions_arr", "netdisk")
	if u, err := url.Parse(authURL); err == nil {
		for _, key := range []string{"state", "qrcode", "force_login"} {
			if value := u.Query().Get(key); value != "" {
				form.Set(key, value)
			}
		}
	}

	body, finalURL, err := session.submitOAuthForm("POST", authURL, form, authURL)
	if err != nil {
		return "", "", err
	}
	if code := codeFromURL(finalURL); code != "" {
		return code, "redirect-url", nil
	}
	if code, source := extractOAuthCode(body); code != "" {
		return code, source, nil
	}

	for i := 0; i < 3; i++ {
		next, ok := nextOAuthForm(body, finalURL, bdstoken, session.Config)
		if !ok {
			break
		}
		body, finalURL, err = session.submitOAuthForm(next.Method, next.Action, next.Fields, finalURL)
		if err != nil {
			return "", "", err
		}
		if code := codeFromURL(finalURL); code != "" {
			return code, "confirm-redirect-url", nil
		}
		if code, source := extractOAuthCode(body); code != "" {
			return code, "confirm-" + source, nil
		}
	}

	if code, source := extractOAuthCode(body); code != "" {
		return code, source, nil
	}
	return "", "", fmt.Errorf("百度授权成功后未找到 code: %s", oauthDebugSummary(body, finalURL))
}

func (session *BaiduQRSession) submitOAuthForm(method, rawURL string, form url.Values, referer string) (string, string, error) {
	var body io.Reader
	target := rawURL
	if strings.EqualFold(method, "GET") {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", "", err
		}
		q := u.Query()
		for key, values := range form {
			for _, value := range values {
				q.Add(key, value)
			}
		}
		u.RawQuery = q.Encode()
		target = u.String()
	} else {
		method = "POST"
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		return "", "", err
	}
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Origin", "https://openapi.baidu.com")
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", baiduUserAgent)
	resp, err := session.Client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("baidu http %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return string(bodyBytes), resp.Request.URL.String(), nil
}

const baiduUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126 Safari/537.36"

func (session *BaiduQRSession) baiduGET(rawURL, referer string) (*http.Response, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", baiduUserAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return session.Client.Do(req)
}

func (session *BaiduQRSession) baiduGETText(rawURL, referer string) (string, error) {
	resp, err := session.baiduGET(rawURL, referer)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("baidu http %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

func parseJSONP(s string, out interface{}) error {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		if j := strings.LastIndexByte(s, ')'); j > i {
			s = s[i+1 : j]
		}
	}
	return json.Unmarshal([]byte(s), out)
}

func normalizeBaiduURL(raw string) string {
	raw = strings.ReplaceAll(raw, `\/`, `/`)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return "https://" + raw
}

func (session *BaiduQRSession) continueAuthURL(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\/`, `/`))
	if raw == "" || raw == "<nil>" {
		return session.AuthURL
	}
	if decoded, err := url.QueryUnescape(raw); err == nil && strings.HasPrefix(decoded, "http") {
		raw = decoded
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return session.AuthURL
	}
	if u.Host != "openapi.baidu.com" {
		return session.AuthURL
	}
	return raw
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func strconvNow() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond))
}

func codeFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Query().Get("error") != "" || u.Query().Get("error_code") != "" {
		return ""
	}
	code := strings.TrimSpace(u.Query().Get("code"))
	if looksLikeOAuthCode(code) {
		return code
	}
	return ""
}

type oauthForm struct {
	Action string
	Method string
	Fields url.Values
	Score  int
}

func nextOAuthForm(body, baseURL, bdstoken string, cfg Config) (oauthForm, bool) {
	forms := parseHTMLForms(body, baseURL)
	if len(forms) == 0 {
		form, ok := fallbackOAuthScopeForm(body, baseURL, bdstoken, cfg)
		return form, ok
	}
	best := oauthForm{Score: -1}
	for _, form := range forms {
		score := form.Score
		if strings.Contains(form.Action, "openapi.baidu.com/oauth/2.0/authorize") {
			score += 8
		}
		if form.Fields.Get("bdstoken") != "" {
			score += 6
		}
		if form.Fields.Get("client_id") == cfg.AppKey {
			score += 4
		}
		if form.Fields.Get("response_type") == "code" {
			score += 3
		}
		if form.Fields.Get("redirect_uri") != "" || cfg.RedirectURI != "" {
			score += 1
		}
		if score > best.Score {
			best = form
			best.Score = score
		}
	}
	if best.Score < 4 {
		form, ok := fallbackOAuthScopeForm(body, baseURL, bdstoken, cfg)
		return form, ok
	}
	fillOAuthFormDefaults(best.Fields, bdstoken, cfg)
	best.Action = oauthAuthorizeAction(best.Action)
	return best, true
}

func parseHTMLForms(body, baseURL string) []oauthForm {
	formRe := regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	matches := formRe.FindAllStringSubmatch(body, -1)
	var forms []oauthForm
	for _, match := range matches {
		attrs := parseTagAttrs(match[1])
		form := oauthForm{
			Action: resolveHTMLAction(attrs["action"], baseURL),
			Method: strings.ToUpper(strings.TrimSpace(attrs["method"])),
			Fields: url.Values{},
		}
		if form.Method == "" {
			form.Method = "GET"
		}
		collectFormFieldsHTML(match[2], form.Fields, &form.Score)
		if form.Action == "" {
			form.Action = baseURL
		}
		forms = append(forms, form)
	}
	return forms
}

func collectFormFieldsHTML(fragment string, fields url.Values, score *int) {
	inputRe := regexp.MustCompile(`(?is)<input\b([^>]*)>`)
	for _, match := range inputRe.FindAllStringSubmatch(fragment, -1) {
		addInputAttrs(parseTagAttrs(match[1]), fields, score)
	}

	textareaRe := regexp.MustCompile(`(?is)<textarea\b([^>]*)>(.*?)</textarea>`)
	for _, match := range textareaRe.FindAllStringSubmatch(fragment, -1) {
		attrs := parseTagAttrs(match[1])
		if name := strings.TrimSpace(attrs["name"]); name != "" && !hasTagAttr(attrs, "disabled") {
			fields.Set(name, htmlText(match[2]))
		}
	}

	selectRe := regexp.MustCompile(`(?is)<select\b([^>]*)>(.*?)</select>`)
	for _, match := range selectRe.FindAllStringSubmatch(fragment, -1) {
		addSelectHTML(parseTagAttrs(match[1]), match[2], fields)
	}

	buttonRe := regexp.MustCompile(`(?is)<button\b([^>]*)>(.*?)</button>`)
	for _, match := range buttonRe.FindAllStringSubmatch(fragment, -1) {
		attrs := parseTagAttrs(match[1])
		if attrs["value"] == "" {
			attrs["value"] = htmlText(match[2])
		}
		addSubmitAttrs(attrs, fields, score)
	}
}

func addInputAttrs(attrs map[string]string, fields url.Values, score *int) {
	name := strings.TrimSpace(attrs["name"])
	if name == "" || hasTagAttr(attrs, "disabled") {
		return
	}
	typ := strings.ToLower(strings.TrimSpace(attrs["type"]))
	value := attrs["value"]
	switch typ {
	case "button", "reset", "file":
		return
	case "checkbox", "radio":
		if hasTagAttr(attrs, "checked") {
			fields.Set(name, value)
		}
	case "submit", "image":
		addNamedSubmit(name, value, fields, score)
	default:
		fields.Set(name, value)
	}
}

func addSubmitAttrs(attrs map[string]string, fields url.Values, score *int) {
	typ := strings.ToLower(strings.TrimSpace(attrs["type"]))
	if typ != "" && typ != "submit" {
		return
	}
	name := strings.TrimSpace(attrs["name"])
	if name == "" || hasTagAttr(attrs, "disabled") {
		return
	}
	addNamedSubmit(name, attrs["value"], fields, score)
}

func addNamedSubmit(name, value string, fields url.Values, score *int) {
	lower := strings.ToLower(name + " " + value)
	if strings.Contains(lower, "cancel") || strings.Contains(lower, "deny") || strings.Contains(lower, "reject") || strings.Contains(value, "取消") || strings.Contains(value, "拒绝") {
		return
	}
	if strings.Contains(lower, "confirm") || strings.Contains(lower, "authorize") || strings.Contains(lower, "allow") || strings.Contains(value, "同意") || strings.Contains(value, "授权") {
		fields.Set(name, value)
		*score += 4
		return
	}
	if fields.Get(name) == "" {
		fields.Set(name, value)
	}
}

func addSelectHTML(attrs map[string]string, inner string, fields url.Values) {
	name := strings.TrimSpace(attrs["name"])
	if name == "" || hasTagAttr(attrs, "disabled") {
		return
	}
	value := ""
	optionRe := regexp.MustCompile(`(?is)<option\b([^>]*)>(.*?)</option>`)
	for _, match := range optionRe.FindAllStringSubmatch(inner, -1) {
		optionAttrs := parseTagAttrs(match[1])
		if hasTagAttr(optionAttrs, "disabled") {
			continue
		}
		optionValue := optionAttrs["value"]
		if optionValue == "" {
			optionValue = htmlText(match[2])
		}
		if value == "" || hasTagAttr(optionAttrs, "selected") {
			value = optionValue
		}
		if hasTagAttr(optionAttrs, "selected") {
			break
		}
	}
	fields.Set(name, value)
}

func fillOAuthFormDefaults(fields url.Values, bdstoken string, cfg Config) {
	if bdstoken != "" {
		fields.Set("bdstoken", bdstoken)
	}
	if fields.Get("client_id") == "" {
		fields.Set("client_id", cfg.AppKey)
	}
	if fields.Get("redirect_uri") == "" {
		fields.Set("redirect_uri", cfg.RedirectURI)
	}
	if fields.Get("response_type") == "" {
		fields.Set("response_type", "code")
	}
	if fields.Get("scope") == "" && fields.Get("grant_permissions") == "" {
		fields.Set("scope", "basic,netdisk")
	}
	fields.Set("grant_permissions", "basic,netdisk")
	fields.Del("grant_permissions_arr")
	fields.Add("grant_permissions_arr", "basic")
	fields.Add("grant_permissions_arr", "netdisk")
}

func fallbackOAuthScopeForm(body, baseURL, bdstoken string, cfg Config) (oauthForm, bool) {
	if !strings.Contains(body, "scope-form") && !strings.Contains(body, "grant_permissions_arr") {
		return oauthForm{}, false
	}
	fields := url.Values{}
	fillOAuthFormDefaults(fields, bdstoken, cfg)
	fields.Set("display", "tv")
	return oauthForm{
		Action: oauthAuthorizeAction(resolveHTMLAction("", baseURL)),
		Method: "POST",
		Fields: fields,
		Score:  4,
	}, true
}

func oauthAuthorizeAction(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Del("confirm_login")
	u.RawQuery = q.Encode()
	return u.String()
}

func resolveHTMLAction(action, baseURL string) string {
	action = strings.TrimSpace(strings.ReplaceAll(action, `\/`, `/`))
	if action == "" {
		return baseURL
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return action
	}
	ref, err := url.Parse(action)
	if err != nil {
		return baseURL
	}
	return base.ResolveReference(ref).String()
}

func extractOAuthCode(body string) (string, string) {
	patterns := []struct {
		name string
		re   string
	}{
		{"body-url", `[?&]code=([A-Za-z0-9._/-]{16,})`},
		{"location-href", `(?i)location(?:\.href)?\s*=\s*["'][^"']*[?&]code=([A-Za-z0-9._/-]{16,})`},
		{"meta-refresh", `(?i)<meta[^>]+url=[^"'>]*[?&]code=([A-Za-z0-9._/-]{16,})`},
		{"oob-title", `(?is)<title>\s*([A-Za-z0-9._/-]{16,})\s*</title>`},
		{"oob-input", `(?is)<(?:input|textarea)\b[^>]*(?:id|name|class)=["'][^"']*(?:verifier|verify|code)[^"']*["'][^>]*\bvalue=["']([A-Za-z0-9._/-]{16,})["']`},
		{"oob-label", `授权码[^A-Za-z0-9._/-]{0,120}([A-Za-z0-9._/-]{16,})`},
	}
	for _, pattern := range patterns {
		if v := extractValue(body, pattern.re); looksLikeOAuthCode(v) {
			return v, pattern.name
		}
	}
	return "", ""
}

func parseTagAttrs(raw string) map[string]string {
	attrs := map[string]string{}
	attrRe := regexp.MustCompile(`(?is)([A-Za-z_:][-A-Za-z0-9_:.]*)\s*(?:=\s*("[^"]*"|'[^']*'|[^\s"'=<>` + "`" + `]+))?`)
	for _, match := range attrRe.FindAllStringSubmatch(raw, -1) {
		key := strings.ToLower(match[1])
		value := ""
		if len(match) > 2 {
			value = strings.TrimSpace(match[2])
			if len(value) >= 2 {
				quote := value[0]
				if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
					value = value[1 : len(value)-1]
				}
			}
		}
		attrs[key] = stdhtml.UnescapeString(value)
	}
	return attrs
}

func hasTagAttr(attrs map[string]string, key string) bool {
	_, ok := attrs[strings.ToLower(key)]
	return ok
}

func htmlText(raw string) string {
	tagRe := regexp.MustCompile(`(?is)<[^>]+>`)
	return strings.TrimSpace(stdhtml.UnescapeString(tagRe.ReplaceAllString(raw, "")))
}

func oauthDebugSummary(body, finalURL string) string {
	parts := []string{}
	if finalURL != "" {
		parts = append(parts, "url="+safeURL(finalURL))
	}
	if title := extractValue(body, `(?is)<title>\s*(.*?)\s*</title>`); title != "" {
		parts = append(parts, "title="+compact(htmlText(title)))
	}
	forms := parseHTMLForms(body, finalURL)
	if len(forms) > 0 {
		formParts := []string{}
		for i, form := range forms {
			if i >= 4 {
				break
			}
			keys := make([]string, 0, len(form.Fields))
			for key := range form.Fields {
				keys = append(keys, key)
			}
			formParts = append(formParts, fmt.Sprintf("%s %s fields=%s", form.Method, safeURL(form.Action), strings.Join(keys, ",")))
		}
		parts = append(parts, "forms=["+strings.Join(formParts, " | ")+"]")
	}
	text := compact(htmlText(body))
	if text != "" {
		parts = append(parts, "text="+text)
	}
	if len(parts) == 0 {
		return compact(body)
	}
	return strings.Join(parts, "; ")
}

func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return compact(raw)
	}
	if u.Scheme == "" && u.Host == "" {
		return compact(raw)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func looksLikeOAuthCode(code string) bool {
	code = strings.TrimSpace(code)
	if len(code) < 16 || len(code) > 512 {
		return false
	}
	lower := strings.ToLower(code)
	if strings.Contains(lower, "error") || strings.Contains(lower, "errno") || strings.Contains(lower, "http") {
		return false
	}
	return regexp.MustCompile(`^[A-Za-z0-9._/-]+$`).MatchString(code)
}

func extractValue(body, pattern string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(body)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func compact(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 240 {
		return s[:240] + "..."
	}
	return s
}
