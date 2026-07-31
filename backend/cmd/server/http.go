package main

import (
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

const frontendHashedAssetCacheControl = "public, max-age=31536000, immutable"

// corsMiddleware 返回一个 chi 中间件，按白名单匹配 Origin 决定是否回写
// CORS 响应头。
//
// 设计要点：
//   - 不再反射任意 Origin。Origin 必须出现在 allowedOrigins 中才会得到
//     Access-Control-Allow-Origin / Allow-Credentials 的"放行"响应头；
//     不在白名单的跨源请求拿不到这些头，浏览器会拒绝读响应内容。
//   - 同源请求（浏览器不发 Origin 头，或 Origin 等于自己）不需要 CORS 头，
//     直接放行。
//   - 始终带 Vary: Origin，避免反代缓存把 A Origin 的允许头喂给 B Origin。
//   - 对不在白名单的 OPTIONS 预检直接 403，避免被当成"放行"信号。
//
// allowedOrigins 由 config.Server.AllowedOrigins 注入；默认为空 = 完全
// 不允许跨源（最安全的默认值，同源部署不受影响）。
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		o = strings.TrimSpace(o)
		if o == "" || o == "*" {
			// 通配符在带 cookie 的 CORS 下没意义且危险，直接忽略
			continue
		}
		allow[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// 任何走过 CORS 检查的响应都要带 Vary: Origin，避免缓存污染。
			w.Header().Add("Vary", "Origin")

			isAllowedOrigin := false
			if origin != "" {
				_, isAllowedOrigin = allow[origin]
			}

			if isAllowedOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "600")
			}

			if r.Method == http.MethodOptions {
				// 预检请求：只对白名单 Origin 返回 204；否则 403 让浏览器把请求拦下来。
				// 同源场景一般不会触发预检（浏览器只在跨源 + 复杂请求时才发 OPTIONS）。
				if isAllowedOrigin {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if origin != "" {
					http.Error(w, "cors: origin not allowed", http.StatusForbidden)
					return
				}
				// 没带 Origin 的 OPTIONS 不是 CORS 预检（可能是健康检查工具），
				// 直接交给下游处理。
			}

			next.ServeHTTP(w, r)
		})
	}
}

func mountFrontend(r chi.Router) {
	dir, ok := resolveFrontendDir()
	if !ok {
		return
	}
	log.Printf("serving frontend from %s", dir)
	r.NotFound(frontendHandler(dir))
}

func resolveFrontendDir() (string, bool) {
	candidates := []string{}
	if dir := strings.TrimSpace(os.Getenv("VIDEO_FRONTEND_DIR")); dir != "" {
		candidates = append(candidates, dir)
	} else {
		candidates = append(candidates, "./dist", "../dist")
	}
	for _, dir := range candidates {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		indexPath := filepath.Join(dir, "index.html")
		if st, err := os.Stat(indexPath); err == nil && !st.IsDir() {
			return dir, true
		}
	}
	return "", false
}

func frontendHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if isBackendRoute(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		cleanPath := path.Clean("/" + r.URL.Path)
		rel := strings.TrimPrefix(cleanPath, "/")
		if rel != "" && rel != "." {
			name := filepath.FromSlash(rel)
			f, err := os.Open(filepath.Join(dir, name))
			if err == nil {
				defer f.Close()
				if st, statErr := f.Stat(); statErr == nil && !st.IsDir() {
					if strings.HasPrefix(cleanPath, "/assets/") {
						w.Header().Set("Cache-Control", frontendHashedAssetCacheControl)
					}
					http.ServeContent(w, r, st.Name(), st.ModTime(), f)
					return
				}
			}
			if filepath.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
		}

		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	}
}

func isBackendRoute(p string) bool {
	return p == "/api" ||
		strings.HasPrefix(p, "/api/") ||
		p == "/admin/api" ||
		strings.HasPrefix(p, "/admin/api/") ||
		p == "/p" ||
		strings.HasPrefix(p, "/p/")
}

func parseBoolDefault(raw string, def bool) bool {
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

func parseIntDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}
