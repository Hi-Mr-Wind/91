package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

const (
	sessionCookie      = "vs_admin"
	sessionTTL         = 24 * time.Hour
	loginFailWindow    = 30 * time.Minute
	loginFailThreshold = 3
)

var ErrLoginIPBanned = errors.New("login ip banned")

type Authenticator struct {
	Username string
	Password string
	Catalog  *catalog.Catalog
	Now      func() time.Time

	credMu   sync.RWMutex
	mu       sync.Mutex
	failures map[string]loginFailure
}

type loginFailure struct {
	Count int
	First time.Time
}

func (a *Authenticator) Login(w http.ResponseWriter, r *http.Request, user, pass string) (bool, error) {
	expectedUser, expectedPass := a.Credentials()
	ip := clientIP(r)
	if ip != "" {
		banned, err := a.Catalog.IsLoginIPBanned(r.Context(), ip)
		if err != nil {
			return false, err
		}
		if banned {
			return false, ErrLoginIPBanned
		}
	}
	if subtle.ConstantTimeCompare([]byte(user), []byte(expectedUser)) != 1 ||
		subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPass)) != 1 {
		if ip != "" {
			if err := a.recordFailure(r, ip); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	if ip != "" {
		a.clearFailures(ip)
	}
	token, err := randomToken()
	if err != nil {
		return false, err
	}
	if err := a.Catalog.CreateSession(r.Context(), token, sessionTTL); err != nil {
		return false, err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
	return true, nil
}

func (a *Authenticator) Credentials() (string, string) {
	a.credMu.RLock()
	defer a.credMu.RUnlock()
	return a.Username, a.Password
}

func (a *Authenticator) SetCredentials(username, password string) {
	a.credMu.Lock()
	defer a.credMu.Unlock()
	a.Username = username
	a.Password = password
}

func (a *Authenticator) recordFailure(r *http.Request, ip string) error {
	now := a.now()
	a.mu.Lock()
	if a.failures == nil {
		a.failures = make(map[string]loginFailure)
	}
	f := a.failures[ip]
	if f.First.IsZero() || now.Sub(f.First) > loginFailWindow {
		f = loginFailure{First: now}
	}
	f.Count++
	a.failures[ip] = f
	shouldBan := f.Count > loginFailThreshold
	a.mu.Unlock()

	if !shouldBan {
		return nil
	}
	if err := a.Catalog.BanLoginIP(r.Context(), ip, "too many failed login attempts"); err != nil {
		return err
	}
	return ErrLoginIPBanned
}

func (a *Authenticator) clearFailures(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.failures, ip)
}

func (a *Authenticator) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Authenticator) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.Catalog.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookie,
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0),
	})
}

func (a *Authenticator) Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ok, err := a.Catalog.ValidateSession(r.Context(), c.Value)
		if err != nil || !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func clientIP(r *http.Request) string {
	for _, candidate := range forwardedIPs(r.Header.Get("X-Forwarded-For")) {
		if isValidIP(candidate) {
			return candidate
		}
	}
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); isValidIP(ip) {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && isValidIP(host) {
		return host
	}
	if isValidIP(r.RemoteAddr) {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return ""
}

func forwardedIPs(header string) []string {
	if strings.TrimSpace(header) == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, strings.TrimSpace(part))
	}
	return out
}

func isValidIP(ip string) bool {
	_, err := netip.ParseAddr(strings.TrimSpace(ip))
	return err == nil
}
