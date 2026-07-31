package p115

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	sdk "github.com/SheltonZhu/115driver/pkg/driver"
)

// QRConfig configures the short-lived client used by the admin QR login flow.
// HTTPClient is injectable so callers can share transport/proxy settings and tests
// can exercise the SDK without contacting 115.
type QRConfig struct {
	HTTPClient *http.Client
}

type QRClient struct {
	client *sdk.Pan115Client
}

// QRCodeSession contains only the temporary values required to poll a 115 QR
// login. The actual credential is returned only after the user confirms in the
// 115 app.
type QRCodeSession struct {
	UID            string `json:"uid"`
	Time           int64  `json:"time"`
	Sign           string `json:"sign"`
	QRCodeURL      string `json:"qrCodeUrl"`
	QRImageDataURL string `json:"qrImageDataUrl"`
}

type QRCodeStatus struct {
	State      string `json:"state"`
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	Cookie     string `json:"cookie,omitempty"`
}

func NewQRClient(c QRConfig) *QRClient {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	// WithClient must run before UA: SetHttpClient replaces the underlying resty
	// client, so applying UA afterwards ensures the 115 browser identity is kept.
	client := sdk.New(
		sdk.WithClient(httpClient),
		sdk.UA(sdk.UA115Browser),
	)
	return &QRClient{client: client}
}

func (c *QRClient) Generate(ctx context.Context) (QRCodeSession, error) {
	if err := ctx.Err(); err != nil {
		return QRCodeSession{}, err
	}
	session, err := c.client.QRCodeStart()
	if err != nil {
		return QRCodeSession{}, fmt.Errorf("115 qr: generate: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return QRCodeSession{}, err
	}
	if session == nil || strings.TrimSpace(session.UID) == "" ||
		strings.TrimSpace(session.Sign) == "" || session.Time <= 0 ||
		strings.TrimSpace(session.QrcodeContent) == "" {
		return QRCodeSession{}, errors.New("115 qr: incomplete session returned by upstream")
	}
	png, err := session.QRCode()
	if err != nil {
		return QRCodeSession{}, fmt.Errorf("115 qr: encode image: %w", err)
	}
	return QRCodeSession{
		UID:            session.UID,
		Time:           session.Time,
		Sign:           session.Sign,
		QRCodeURL:      session.QrcodeContent,
		QRImageDataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	}, nil
}

// Poll checks the scan state and exchanges an approved session for the same web
// Cookie format already consumed by Driver.Init (UID/CID/SEID/KID).
func (c *QRClient) Poll(ctx context.Context, uid string, timestamp int64, sign string) (QRCodeStatus, error) {
	uid = strings.TrimSpace(uid)
	sign = strings.TrimSpace(sign)
	if uid == "" {
		return QRCodeStatus{}, errors.New("uid is required")
	}
	if timestamp <= 0 {
		return QRCodeStatus{}, errors.New("time must be greater than zero")
	}
	if sign == "" {
		return QRCodeStatus{}, errors.New("sign is required")
	}
	if err := ctx.Err(); err != nil {
		return QRCodeStatus{}, err
	}

	session := &sdk.QRCodeSession{UID: uid, Time: timestamp, Sign: sign}
	status, err := c.client.QRCodeStatus(session)
	if err != nil {
		if errors.Is(err, sdk.ErrQrcodeExpired) {
			return QRCodeStatus{State: "expired", Status: -1, StatusText: "二维码已过期"}, nil
		}
		return QRCodeStatus{}, fmt.Errorf("115 qr: query status: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return QRCodeStatus{}, err
	}
	if status == nil {
		return QRCodeStatus{}, errors.New("115 qr: empty status returned by upstream")
	}

	switch {
	case status.IsWaiting():
		return QRCodeStatus{State: "waiting", Status: status.Status, StatusText: "等待使用 115 App 扫码"}, nil
	case status.IsScanned():
		return QRCodeStatus{State: "scanned", Status: status.Status, StatusText: "已扫码，请在 115 App 确认登录"}, nil
	case status.IsExpired():
		return QRCodeStatus{State: "expired", Status: status.Status, StatusText: "二维码已过期"}, nil
	case status.IsCanceled():
		return QRCodeStatus{State: "canceled", Status: status.Status, StatusText: "已取消登录"}, nil
	case status.IsAllowed():
		credential, loginErr := c.client.QRCodeLoginWithApp(session, sdk.LoginAppWeb)
		if loginErr != nil {
			return QRCodeStatus{}, fmt.Errorf("115 qr: exchange credential: %w", loginErr)
		}
		if err := ctx.Err(); err != nil {
			return QRCodeStatus{}, err
		}
		if credential == nil {
			return QRCodeStatus{}, errors.New("115 qr: empty credential returned by upstream")
		}
		cookie := credential.Cookie()
		var parsed sdk.Credential
		if err := parsed.FromCookie(cookie); err != nil {
			return QRCodeStatus{}, fmt.Errorf("115 qr: incomplete credential returned by upstream: %w", err)
		}
		return QRCodeStatus{
			State:      "success",
			Status:     status.Status,
			StatusText: "登录成功",
			Cookie:     cookie,
		}, nil
	default:
		return QRCodeStatus{
			State:      "error",
			Status:     status.Status,
			StatusText: fmt.Sprintf("未知扫码状态（%d）", status.Status),
		}, nil
	}
}
