package quark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestQRClientGenerateUsesOfficialParameters(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	var generateCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		generateCalls++
		if r.Method != http.MethodGet || r.URL.Path != "/cas/ajax/getTokenForQrcodeLogin" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("client_id") != qrClientID || r.URL.Query().Get("v") != qrAPIVersion {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if requestID := r.URL.Query().Get("request_id"); requestID == "" {
			t.Error("request_id is empty")
		}
		if r.Header.Get("Origin") != defaultQRPanBaseURL || r.Header.Get("Referer") != defaultQRPanBaseURL+"/" {
			t.Errorf("login headers = %#v", r.Header)
		}
		http.SetCookie(w, &http.Cookie{Name: "_UP_LOGIN", Value: "temporary", Path: "/", HttpOnly: true})
		writeQRTestJSON(w, map[string]any{
			"status":  qrSuccessStatus,
			"message": "ok",
			"data": map[string]any{
				"members": map[string]string{"token": "qr-token"},
			},
		})
	}))
	defer upstream.Close()

	client := NewQRClient(QRConfig{
		UOPBaseURL: upstream.URL,
		PanBaseURL: upstream.URL,
		Now:        func() time.Time { return now },
	})
	session, err := client.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if generateCalls != 1 {
		t.Fatalf("generate calls = %d, want 1", generateCalls)
	}
	if session.Token != "qr-token" {
		t.Fatalf("token = %q", session.Token)
	}
	if session.ExpiresAt != now.Add(qrSessionTTL).Format(time.RFC3339) {
		t.Fatalf("expiresAt = %q", session.ExpiresAt)
	}
	if !strings.HasPrefix(session.QRImageDataURL, "data:image/png;base64,") {
		t.Fatalf("qr image = %q", session.QRImageDataURL)
	}
	qrURL, err := url.Parse(session.QRCodeURL)
	if err != nil {
		t.Fatalf("parse qr url: %v", err)
	}
	if qrURL.Scheme+"://"+qrURL.Host+qrURL.Path != defaultQRScanURL {
		t.Fatalf("qr url = %q", session.QRCodeURL)
	}
	query := qrURL.Query()
	if query.Get("token") != "qr-token" || query.Get("client_id") != qrClientID || query.Get("ssb") != "weblogin" {
		t.Fatalf("qr query = %s", qrURL.RawQuery)
	}
	if query.Get("uc_biz_str") != "S:custom|OPT:SAREA@0|OPT:IMMERSIVE@1|OPT:BACK_BTN_STYLE@0" {
		t.Fatalf("uc_biz_str = %q", query.Get("uc_biz_str"))
	}
}

func TestQRClientPollWaitingThenExchangesCookieOnce(t *testing.T) {
	var pollCalls int
	var accountCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cas/ajax/getTokenForQrcodeLogin":
			http.SetCookie(w, &http.Cookie{Name: "_UP_LOGIN", Value: "temporary", Path: "/", HttpOnly: true})
			writeQRTestJSON(w, map[string]any{
				"status": qrSuccessStatus,
				"data":   map[string]any{"members": map[string]string{"token": "qr-token"}},
			})
		case "/cas/ajax/getServiceTicketByQrcodeToken":
			pollCalls++
			if r.URL.Query().Get("token") != "qr-token" {
				t.Errorf("poll token = %q", r.URL.Query().Get("token"))
			}
			if !strings.Contains(r.Header.Get("Cookie"), "_UP_LOGIN=temporary") {
				t.Errorf("poll cookie = %q", r.Header.Get("Cookie"))
			}
			if pollCalls == 1 {
				writeQRTestJSON(w, map[string]any{"status": qrWaitingStatus, "message": "Query result is empty"})
				return
			}
			writeQRTestJSON(w, map[string]any{
				"status": qrSuccessStatus,
				"data":   map[string]any{"members": map[string]string{"service_ticket": "service-ticket"}},
			})
		case "/account/info":
			accountCalls++
			if r.URL.Query().Get("st") != "service-ticket" {
				t.Errorf("service ticket = %q", r.URL.Query().Get("st"))
			}
			if !strings.Contains(r.Header.Get("Cookie"), "_UP_LOGIN=temporary") {
				t.Errorf("account cookie = %q", r.Header.Get("Cookie"))
			}
			http.SetCookie(w, &http.Cookie{Name: "__pus", Value: "pus-value", Path: "/", HttpOnly: true})
			http.SetCookie(w, &http.Cookie{Name: "__puus", Value: "puus-value", Path: "/", HttpOnly: true})
			http.SetCookie(w, &http.Cookie{Name: "other", Value: "keep", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "_UP_LOGIN", Value: "stale", Path: "/", HttpOnly: true})
			writeQRTestJSON(w, map[string]any{"success": true, "code": 0, "data": map[string]string{"nickname": "tester"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	client := NewQRClient(QRConfig{UOPBaseURL: upstream.URL, PanBaseURL: upstream.URL})
	session, err := client.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	waiting, err := client.Poll(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Poll waiting: %v", err)
	}
	if waiting.State != "waiting" || waiting.Status != qrWaitingStatus || waiting.Cookie != "" {
		t.Fatalf("waiting = %#v", waiting)
	}

	success, err := client.Poll(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Poll success: %v", err)
	}
	wantCookie := "__pus=pus-value; __puus=puus-value; other=keep"
	if success.State != "success" || success.Cookie != wantCookie {
		t.Fatalf("success = %#v, want cookie %q", success, wantCookie)
	}
	if strings.Contains(success.Cookie, "_UP_LOGIN") {
		t.Fatalf("temporary cookie leaked into credential: %q", success.Cookie)
	}
	if pollCalls != 2 || accountCalls != 1 {
		t.Fatalf("calls after success: poll=%d account=%d", pollCalls, accountCalls)
	}

	repeated, err := client.Poll(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Poll repeated: %v", err)
	}
	if repeated.State != "success" || repeated.Cookie != wantCookie {
		t.Fatalf("repeated = %#v", repeated)
	}
	if pollCalls != 2 || accountCalls != 1 {
		t.Fatalf("repeated poll called upstream: poll=%d account=%d", pollCalls, accountCalls)
	}
}

func TestQRClientPollExpiresServerSideSession(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	var pollCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cas/ajax/getTokenForQrcodeLogin":
			writeQRTestJSON(w, map[string]any{
				"status": qrSuccessStatus,
				"data":   map[string]any{"members": map[string]string{"token": "qr-token"}},
			})
		case "/cas/ajax/getServiceTicketByQrcodeToken":
			pollCalls++
			writeQRTestJSON(w, map[string]any{"status": qrWaitingStatus})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	client := NewQRClient(QRConfig{
		UOPBaseURL: upstream.URL,
		PanBaseURL: upstream.URL,
		Now:        func() time.Time { return now },
	})
	session, err := client.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	now = now.Add(qrSessionTTL)
	status, err := client.Poll(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if status.State != "expired" || status.Status != qrExpiredStatus {
		t.Fatalf("status = %#v", status)
	}
	if pollCalls != 0 {
		t.Fatalf("expired session made %d upstream poll calls", pollCalls)
	}
}

func TestQRClientPollRejectsMissingCredentialCookie(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cas/ajax/getTokenForQrcodeLogin":
			writeQRTestJSON(w, map[string]any{
				"status": qrSuccessStatus,
				"data":   map[string]any{"members": map[string]string{"token": "qr-token"}},
			})
		case "/cas/ajax/getServiceTicketByQrcodeToken":
			writeQRTestJSON(w, map[string]any{
				"status": qrSuccessStatus,
				"data":   map[string]any{"members": map[string]string{"service_ticket": "service-ticket"}},
			})
		case "/account/info":
			writeQRTestJSON(w, map[string]any{"success": true, "code": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	client := NewQRClient(QRConfig{UOPBaseURL: upstream.URL, PanBaseURL: upstream.URL})
	session, err := client.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := client.Poll(context.Background(), session.Token); err == nil || !strings.Contains(err.Error(), "credential cookie is missing") {
		t.Fatalf("Poll error = %v", err)
	}
}

func writeQRTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
