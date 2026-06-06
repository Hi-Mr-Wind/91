package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

func TestLoginBansIPAfterMoreThanThreeFailuresInThirtyMinutes(t *testing.T) {
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Unix(1_700_000_000, 0)
	authr := &Authenticator{
		Username: "admin",
		Password: "secret",
		Catalog:  cat,
		Now:      func() time.Time { return now },
	}

	for i := 0; i < loginFailThreshold; i++ {
		ok, err := authr.Login(httptest.NewRecorder(), loginRequest("203.0.113.10"), "admin", "wrong")
		if err != nil {
			t.Fatalf("failure %d returned error: %v", i+1, err)
		}
		if ok {
			t.Fatalf("failure %d returned ok", i+1)
		}
	}

	ok, err := authr.Login(httptest.NewRecorder(), loginRequest("203.0.113.10"), "admin", "wrong")
	if ok {
		t.Fatal("fourth failed login returned ok")
	}
	if !errors.Is(err, ErrLoginIPBanned) {
		t.Fatalf("fourth failed login error = %v, want ErrLoginIPBanned", err)
	}

	banned, err := cat.IsLoginIPBanned(loginRequest("203.0.113.10").Context(), "203.0.113.10")
	if err != nil {
		t.Fatalf("query ban: %v", err)
	}
	if !banned {
		t.Fatal("ip was not persisted as banned")
	}

	reloaded := &Authenticator{Username: "admin", Password: "secret", Catalog: cat}
	ok, err = reloaded.Login(httptest.NewRecorder(), loginRequest("203.0.113.10"), "admin", "secret")
	if ok {
		t.Fatal("banned ip logged in with correct credentials")
	}
	if !errors.Is(err, ErrLoginIPBanned) {
		t.Fatalf("banned ip error = %v, want ErrLoginIPBanned", err)
	}
}

func TestSuccessfulLoginClearsFailedLoginWindow(t *testing.T) {
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	authr := &Authenticator{
		Username: "admin",
		Password: "secret",
		Catalog:  cat,
	}

	for i := 0; i < loginFailThreshold; i++ {
		if ok, err := authr.Login(httptest.NewRecorder(), loginRequest("203.0.113.11"), "admin", "wrong"); err != nil || ok {
			t.Fatalf("failed login %d ok=%v err=%v", i+1, ok, err)
		}
	}
	if ok, err := authr.Login(httptest.NewRecorder(), loginRequest("203.0.113.11"), "admin", "secret"); err != nil || !ok {
		t.Fatalf("successful login after failures ok=%v err=%v", ok, err)
	}
	if ok, err := authr.Login(httptest.NewRecorder(), loginRequest("203.0.113.11"), "admin", "wrong"); err != nil || ok {
		t.Fatalf("failure after successful login ok=%v err=%v", ok, err)
	}
}

func TestClientIPUsesForwardedHeaders(t *testing.T) {
	req := loginRequest("198.51.100.20")
	req.Header.Set("X-Forwarded-For", "203.0.113.12, 198.51.100.20")

	if got := clientIP(req); got != "203.0.113.12" {
		t.Fatalf("client IP = %q, want forwarded origin", got)
	}
}

func loginRequest(ip string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(`{}`))
	req.RemoteAddr = ip + ":12345"
	return req
}
