package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"novel-mcp/internal/atomicfile"
)

// Credentials 两项都是 256 bit 随机值：route 走 URL 路径，bearer 走请求头。
// 文件权限 0600，且永不写进日志、工具结果或 Git。
type Credentials struct {
	Route  string `json:"route"`
	Bearer string `json:"bearer"`
}

func LoadCredentials(file string) (Credentials, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return Credentials{}, err
	}
	if !info.Mode().IsRegular() {
		return Credentials{}, errors.New("凭据文件必须是普通文件")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, errors.New("凭据文件已损坏")
	}
	for _, v := range []string{c.Route, c.Bearer} {
		raw, err := hex.DecodeString(v)
		if err != nil || len(raw) != 32 {
			return Credentials{}, errors.New("凭据必须是 64 位十六进制（256 bit）")
		}
	}
	// 两个通道同值说明这是手工填的占位凭据：路径令牌会进 URL、访问日志和浏览器
	// 历史，Bearer 不会，共用等于把两者一起降级。宁可拒载，也不要静默照跑。
	if c.Route == c.Bearer {
		return Credentials{}, errors.New("route 与 bearer 不能相同（运行 novel-mcp token rotate 重新生成）")
	}
	return c, nil
}

// WriteCredentials 用 tmp+sync+rename 落盘：半截凭据文件会让服务起不来，
// 比一次写入失败更难诊断。
func WriteCredentials(file string, rotate bool) (Credentials, error) {
	if !rotate {
		if existing, err := LoadCredentials(file); err == nil {
			return existing, nil
		} else if !os.IsNotExist(err) {
			return Credentials{}, err
		}
	}
	var c Credentials
	for _, v := range []*string{&c.Route, &c.Bearer} {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return Credentials{}, err
		}
		*v = hex.EncodeToString(raw)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return Credentials{}, err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return Credentials{}, err
	}
	data = append(data, '\n')
	return c, atomicfile.Write(file, data, 0o600)
}

type HTTPOptions struct {
	Credentials   func() (Credentials, error)
	Hosts         []string
	Origins       []string
	RequireBearer bool
	Observer      *Observer
}

// ValidPublicURL 只接受 HTTPS 源：不带路径、查询、片段或 userinfo。
// 隧道给的 path 前缀要拼在 /mcp/<route> 之前，所以源之外一律拒绝，避免
// 运维照着打印的地址少抄一段。
func ValidPublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || strings.HasSuffix(u.Host, ":") {
		return nil, errors.New("public_url 必须是 HTTPS 源，不含路径、查询、片段或账号")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, errors.New("public_url 端口必须在 1-65535")
		}
	}
	return u, nil
}

func equalSecret(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func allowed(xs []string, v string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func allowedOrigin(xs []string, origin string) bool {
	for _, x := range xs {
		if x == "*" || strings.EqualFold(x, origin) {
			return true
		}
	}
	return false
}

// NewHTTP 组装唯一的公网面：只认 /healthz 与 /mcp/<route>。控制台、管理 API、
// shell、任意文件访问在这里不存在，所以也不存在“忘了加鉴权”的旁路。
func NewHTTP(p *Projects, opts HTTPOptions) http.Handler {
	server := NewMCP(p, opts.Observer)
	transport := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:           true, // 网页客户端可能换出口 IP；不建会话表就没有可泄漏的会话
			JSONResponse:        true,
			MaxRequestBodyBytes: 4 << 20,
			// 关 SDK 自带的本机保护：它只认 localhost，会拒掉隧道 Host。
			// 上面已用精确 Host 白名单取代，二者取其一，这里取白名单。
			DisableLocalhostProtection: true,
		})
	slots := make(chan struct{}, 16)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		logHTTP := func(event string, status int) {
			if opts.Observer != nil && opts.Observer.CallLog != nil {
				opts.Observer.CallLog.HTTP(event, r.Method, status, r.ContentLength, r.Header.Get("Origin") != "", r.Header.Get("MCP-Protocol-Version"), time.Since(started))
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !allowed(opts.Hosts, r.Host) {
			logHTTP("host_rejected", http.StatusForbidden)
			http.Error(w, "untrusted host", http.StatusForbidden)
			return
		}
		// 空 allow_origins = 只允许同源/非浏览器客户端；"*" = 接受任意浏览器 Origin。
		// Host、route 与 Bearer 仍是独立准入边界。
		origin := r.Header.Get("Origin")
		if origin != "" && !allowedOrigin(opts.Origins, origin) {
			logHTTP("origin_rejected", http.StatusForbidden)
			http.Error(w, "untrusted origin", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			logHTTP("health", http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"service":"novel-mcp","auth":"` + authMode(opts.RequireBearer) + `"}`))
			return
		}
		creds, err := opts.Credentials()
		if err != nil {
			logHTTP("credentials_unavailable", http.StatusServiceUnavailable)
			http.Error(w, "credentials unavailable", http.StatusServiceUnavailable)
			return
		}
		// 先判路径再判 bearer：不知道 route 时统一只能得到 404；拿到正确 route 后，
		// Bearer 按标准 HTTP 语义返回 401。不重定向、不回显路径，也不开放 /mcp。
		if r.URL.RawQuery != "" || r.URL.Fragment != "" || !equalSecret(r.URL.Path, "/mcp/"+creds.Route) {
			logHTTP("route_not_found", http.StatusNotFound)
			http.NotFound(w, r)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version, Mcp-Session-Id, Last-Event-ID")
		}
		if r.Method == http.MethodOptions {
			logHTTP("preflight", http.StatusNoContent)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if opts.RequireBearer {
			scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
			if !strings.EqualFold(scheme, "Bearer") || !equalSecret(strings.TrimSpace(token), creds.Bearer) {
				logHTTP("auth_failed", http.StatusUnauthorized)
				w.Header().Set("WWW-Authenticate", `Bearer realm="novel-mcp"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
		}
		if r.Method != http.MethodPost {
			logHTTP("method_rejected", http.StatusMethodNotAllowed)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			logHTTP("busy", http.StatusTooManyRequests)
			http.Error(w, "server busy; retry later", http.StatusTooManyRequests)
			return
		}
		logHTTP("mcp_post", 0)
		transport.ServeHTTP(w, r)
	})
}

// healthz 是公网可探的，因此只回答“活着吗”，不回答项目数或路径。
// auth 字段是给运维看的一行事实：public-open 就是把 URL 当唯一凭据。
func authMode(requireBearer bool) string {
	if requireBearer {
		return "bearer"
	}
	return "public-open"
}
