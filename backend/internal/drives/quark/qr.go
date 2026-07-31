package quark

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	"golang.org/x/net/publicsuffix"
)

const (
	defaultQRUOPBaseURL = "https://uop.quark.cn"
	defaultQRPanBaseURL = "https://pan.quark.cn"
	defaultQRScanURL    = "https://su.quark.cn/4_eMHBJ"
	defaultQRUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"
	qrClientID          = "532"
	qrAPIVersion        = "1.2"
	qrSuccessStatus     = 2000000
	qrWaitingStatus     = 50004001
	qrExpiredStatus     = 50004002
	qrSessionTTL        = 10 * time.Minute
	maxQRSessions       = 32
)

// QRConfig configures Quark's web QR-code login flow. The base URLs and HTTP
// client are injectable so tests never need to contact Quark.
type QRConfig struct {
	UOPBaseURL string
	PanBaseURL string
	HTTPClient *http.Client
	Now        func() time.Time
}

// QRClient keeps the official UOP login cookies in short-lived server-side
// sessions. Only the QR token and the final drive Cookie are exposed to the
// admin browser.
type QRClient struct {
	uopBaseURL string
	panBaseURL string
	httpClient *http.Client
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]*qrSession
}

type qrSession struct {
	mu        sync.Mutex
	client    *http.Client
	expiresAt time.Time
	cookie    string
}

type QRCodeSession struct {
	Token          string `json:"token"`
	QRCodeURL      string `json:"qrCodeUrl"`
	QRImageDataURL string `json:"qrImageDataUrl"`
	ExpiresAt      string `json:"expiresAt"`
}

type QRCodeStatus struct {
	State      string `json:"state"`
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	Cookie     string `json:"cookie,omitempty"`
}

type qrUOPResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Members struct {
			Token         string `json:"token"`
			ServiceTicket string `json:"service_ticket"`
		} `json:"members"`
	} `json:"data"`
}

type qrAccountInfoResponse struct {
	Success bool            `json:"success"`
	Code    json.RawMessage `json:"code"`
	Msg     string          `json:"msg"`
}

func NewQRClient(c QRConfig) *QRClient {
	uopBaseURL := strings.TrimRight(strings.TrimSpace(c.UOPBaseURL), "/")
	if uopBaseURL == "" {
		uopBaseURL = defaultQRUOPBaseURL
	}
	panBaseURL := strings.TrimRight(strings.TrimSpace(c.PanBaseURL), "/")
	if panBaseURL == "" {
		panBaseURL = defaultQRPanBaseURL
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &QRClient{
		uopBaseURL: uopBaseURL,
		panBaseURL: panBaseURL,
		httpClient: httpClient,
		now:        now,
		sessions:   make(map[string]*qrSession),
	}
}

func (c *QRClient) Generate(ctx context.Context) (QRCodeSession, error) {
	client, err := c.newSessionHTTPClient()
	if err != nil {
		return QRCodeSession{}, fmt.Errorf("quark qr: create cookie jar: %w", err)
	}

	var out qrUOPResponse
	if err := c.getUOPJSON(ctx, client, "/cas/ajax/getTokenForQrcodeLogin", nil, &out); err != nil {
		return QRCodeSession{}, fmt.Errorf("quark qr: generate: %w", err)
	}
	if out.Status != qrSuccessStatus {
		return QRCodeSession{}, fmt.Errorf("quark qr: generate: upstream status=%d message=%s", out.Status, strings.TrimSpace(out.Message))
	}
	token := strings.TrimSpace(out.Data.Members.Token)
	if token == "" {
		return QRCodeSession{}, errors.New("quark qr: generate: empty token")
	}

	qrURL, err := buildQRLoginURL(token)
	if err != nil {
		return QRCodeSession{}, fmt.Errorf("quark qr: build login url: %w", err)
	}
	png, err := qrcode.Encode(qrURL, qrcode.Medium, 220)
	if err != nil {
		return QRCodeSession{}, fmt.Errorf("quark qr: encode image: %w", err)
	}
	expiresAt := c.now().Add(qrSessionTTL)
	c.storeSession(token, &qrSession{client: client, expiresAt: expiresAt})

	return QRCodeSession{
		Token:          token,
		QRCodeURL:      qrURL,
		QRImageDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		ExpiresAt:      expiresAt.Format(time.RFC3339),
	}, nil
}

func (c *QRClient) Poll(ctx context.Context, token string) (QRCodeStatus, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return QRCodeStatus{}, errors.New("token is required")
	}
	session := c.findSession(token)
	if session == nil {
		return QRCodeStatus{State: "expired", Status: qrExpiredStatus, StatusText: "二维码已过期"}, nil
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if !c.now().Before(session.expiresAt) {
		c.deleteSession(token, session)
		return QRCodeStatus{State: "expired", Status: qrExpiredStatus, StatusText: "二维码已过期"}, nil
	}
	if session.cookie != "" {
		return QRCodeStatus{
			State:      "success",
			Status:     qrSuccessStatus,
			StatusText: "登录成功",
			Cookie:     session.cookie,
		}, nil
	}

	var out qrUOPResponse
	if err := c.getUOPJSON(ctx, session.client, "/cas/ajax/getServiceTicketByQrcodeToken", url.Values{"token": {token}}, &out); err != nil {
		return QRCodeStatus{}, fmt.Errorf("quark qr: query status: %w", err)
	}
	switch out.Status {
	case qrWaitingStatus:
		return QRCodeStatus{State: "waiting", Status: out.Status, StatusText: "等待使用夸克 App 扫码"}, nil
	case qrExpiredStatus:
		c.deleteSession(token, session)
		return QRCodeStatus{State: "expired", Status: out.Status, StatusText: "二维码已过期"}, nil
	case qrSuccessStatus:
		serviceTicket := strings.TrimSpace(out.Data.Members.ServiceTicket)
		if serviceTicket == "" {
			return QRCodeStatus{}, errors.New("quark qr: login succeeded but service ticket is empty")
		}
		if err := c.exchangeServiceTicket(ctx, session.client, serviceTicket); err != nil {
			return QRCodeStatus{}, err
		}
		cookie, err := c.credentialCookie(session.client.Jar)
		if err != nil {
			return QRCodeStatus{}, err
		}
		session.cookie = cookie
		return QRCodeStatus{
			State:      "success",
			Status:     out.Status,
			StatusText: "登录成功",
			Cookie:     cookie,
		}, nil
	default:
		return QRCodeStatus{
			State:      "error",
			Status:     out.Status,
			StatusText: fmt.Sprintf("扫码状态异常（%d）", out.Status),
		}, nil
	}
}

func (c *QRClient) exchangeServiceTicket(ctx context.Context, client *http.Client, serviceTicket string) error {
	var out qrAccountInfoResponse
	if err := c.getJSON(ctx, client, c.panBaseURL+"/account/info", url.Values{"st": {serviceTicket}}, &out); err != nil {
		return fmt.Errorf("quark qr: exchange credential: %w", err)
	}
	if !out.Success {
		message := strings.TrimSpace(out.Msg)
		if message == "" {
			message = "account login failed"
		}
		return fmt.Errorf("quark qr: exchange credential: %s", message)
	}
	return nil
}

func (c *QRClient) newSessionHTTPClient() (*http.Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}
	clone := *c.httpClient
	clone.Jar = jar
	if clone.Timeout <= 0 {
		clone.Timeout = 20 * time.Second
	}
	return &clone, nil
}

func (c *QRClient) getUOPJSON(ctx context.Context, client *http.Client, pathname string, query url.Values, out any) error {
	if query == nil {
		query = make(url.Values)
	} else {
		query = cloneValues(query)
	}
	query.Set("client_id", qrClientID)
	query.Set("v", qrAPIVersion)
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	query.Set("request_id", requestID)
	return c.getJSON(ctx, client, c.uopBaseURL+pathname, query, out)
}

func (c *QRClient) getJSON(ctx context.Context, client *http.Client, rawURL string, query url.Values, out any) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	q := u.Query()
	for key, values := range query {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", defaultQRPanBaseURL)
	req.Header.Set("Referer", defaultQRPanBaseURL+"/")
	req.Header.Set("User-Agent", defaultQRUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(out); err != nil {
		return fmt.Errorf("decode upstream response: %w", err)
	}
	return nil
}

func (c *QRClient) credentialCookie(jar http.CookieJar) (string, error) {
	if jar == nil {
		return "", errors.New("quark qr: credential cookie jar is unavailable")
	}
	accountURL, err := url.Parse(c.panBaseURL + "/account/info")
	if err != nil {
		return "", fmt.Errorf("quark qr: parse account url: %w", err)
	}
	panURL, err := url.Parse(c.panBaseURL + "/")
	if err != nil {
		return "", fmt.Errorf("quark qr: parse pan url: %w", err)
	}

	values := make(map[string]string)
	for _, u := range []*url.URL{accountURL, panURL} {
		for _, item := range jar.Cookies(u) {
			name := strings.TrimSpace(item.Name)
			if name == "" || strings.HasPrefix(strings.ToUpper(name), "_UP_") {
				continue
			}
			values[name] = item.Value
		}
	}
	if values["__pus"] == "" && values["__puus"] == "" {
		return "", errors.New("quark qr: login succeeded but drive credential cookie is missing")
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		pi, pj := cookiePriority(names[i]), cookiePriority(names[j])
		if pi != pj {
			return pi < pj
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, (&http.Cookie{Name: name, Value: values[name]}).String())
	}
	return strings.Join(parts, "; "), nil
}

func (c *QRClient) storeSession(token string, session *qrSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupSessionsLocked()
	if len(c.sessions) >= maxQRSessions {
		var oldestToken string
		var oldestExpiry time.Time
		for existingToken, existing := range c.sessions {
			if oldestToken == "" || existing.expiresAt.Before(oldestExpiry) {
				oldestToken = existingToken
				oldestExpiry = existing.expiresAt
			}
		}
		delete(c.sessions, oldestToken)
	}
	c.sessions[token] = session
}

func (c *QRClient) findSession(token string) *qrSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupSessionsLocked()
	return c.sessions[token]
}

func (c *QRClient) deleteSession(token string, expected *qrSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[token] == expected {
		delete(c.sessions, token)
	}
}

func (c *QRClient) cleanupSessionsLocked() {
	now := c.now()
	for token, session := range c.sessions {
		if !now.Before(session.expiresAt) {
			delete(c.sessions, token)
		}
	}
}

func buildQRLoginURL(token string) (string, error) {
	u, err := url.Parse(defaultQRScanURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("token", token)
	q.Set("client_id", qrClientID)
	q.Set("ssb", "weblogin")
	q.Set("uc_param_str", "")
	q.Set("uc_biz_str", "S:custom|OPT:SAREA@0|OPT:IMMERSIVE@1|OPT:BACK_BTN_STYLE@0")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func newRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate request id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func cloneValues(src url.Values) url.Values {
	dst := make(url.Values, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func cookiePriority(name string) int {
	switch name {
	case "__pus":
		return 0
	case "__puus":
		return 1
	default:
		return 2
	}
}
