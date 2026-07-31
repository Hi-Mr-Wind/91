// Package streamhttp contains the HTTP redirect policy shared by consumers of
// drive StreamLink values.
package streamhttp

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewClient returns a streaming client that follows redirects without leaking
// drive credentials or adding an implicit Referer to a different origin.
//
// Go's net/http client normally synthesizes Referer while following redirects.
// Some signed download endpoints (including WoPan links returned by OpenList's
// WebDAV 302 mode) reject any Referer with HTTP 400. Explicit provider headers
// remain intact; only a Referer synthesized by net/http is removed.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: CheckRedirect,
	}
}

// NewNoRedirectClient returns a client for transparent HTTP relays. The first
// response is returned to the caller even when it is a redirect.
func NewNoRedirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// CheckRedirect is suitable for http.Client.CheckRedirect when fetching a
// StreamLink. Range and other ordinary streaming headers are left untouched.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 || sameOrigin(via[0].URL, req.URL) {
		return nil
	}

	// Preserve a provider-supplied Referer, but remove the source URL that
	// net/http automatically generated during this cross-origin redirect.
	if explicit, ok := via[0].Header["Referer"]; ok {
		req.Header.Del("Referer")
		for _, value := range explicit {
			req.Header.Add("Referer", value)
		}
	} else {
		req.Header.Del("Referer")
	}

	// net/http already avoids forwarding these headers to unrelated hosts.
	// Delete them explicitly as a defense in depth and also cover redirects to
	// the same hostname on a different scheme or port.
	req.Header.Del("Authorization")
	req.Header.Del("Proxy-Authorization")
	req.Header.Del("Cookie")
	return nil
}

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
