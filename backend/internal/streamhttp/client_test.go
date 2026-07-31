package streamhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCrossOriginRedirectDropsImplicitRefererAndCredentials(t *testing.T) {
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != "" {
			t.Errorf("Referer = %q, want empty", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization leaked to redirect target: %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Errorf("Cookie leaked to redirect target: %q", got)
		}
		if got := r.Header.Get("Range"); got != "bytes=2-5" {
			t.Errorf("Range = %q, want bytes=2-5", got)
		}
		if got := r.Header.Get("User-Agent"); got != "video-site-webdav" {
			t.Errorf("User-Agent = %q, want video-site-webdav", got)
		}
		w.Header().Set("Content-Range", "bytes 2-5/10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "2345")
	}))
	t.Cleanup(cdn.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Basic secret" {
			t.Errorf("origin Authorization = %q, want Basic secret", got)
		}
		http.Redirect(w, r, cdn.URL+"/video.mp4", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	req, err := http.NewRequest(http.MethodGet, origin.URL+"/dav/video.mp4", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Basic secret")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("Range", "bytes=2-5")
	req.Header.Set("User-Agent", "video-site-webdav")
	resp, err := NewClient(0).Do(req)
	if err != nil {
		t.Fatalf("redirected request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("response = status %d body %q", resp.StatusCode, body)
	}
}

func TestClientPreservesExplicitProviderRefererAcrossRedirect(t *testing.T) {
	const providerReferer = "https://provider.example/"
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Referer"); got != providerReferer {
			t.Errorf("Referer = %q, want %q", got, providerReferer)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(cdn.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/file", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	req, err := http.NewRequest(http.MethodGet, origin.URL+"/file", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Referer", providerReferer)
	resp, err := NewClient(0).Do(req)
	if err != nil {
		t.Fatalf("redirected request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}
