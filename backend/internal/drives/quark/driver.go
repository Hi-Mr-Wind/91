package quark

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/video-site/backend/internal/drives"
)

const (
	defaultUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch"
	defaultReferer = "https://pan.quark.cn"
	defaultAPI     = "https://drive.quark.cn/1/clouddrive"
	defaultPR      = "ucpro"
)

type Driver struct {
	id                    string
	cookie                string
	rootID                string
	ua                    string
	referer               string
	apiBase               string
	pr                    string
	client                *resty.Client
	onCookieUpdate        func(string)
	useTranscodingAddress bool
}

type Config struct {
	ID                    string
	Cookie                string
	RootID                string
	UseTranscodingAddress bool // 开启后对视频文件返回转码直链（支持 302），但可能画质不一致
	OnCookieUpdate        func(cookie string)
}

func New(c Config) *Driver {
	rootID := c.RootID
	if rootID == "" {
		rootID = "0"
	}
	d := &Driver{
		id:                    c.ID,
		cookie:                c.Cookie,
		rootID:                rootID,
		ua:                    defaultUA,
		referer:               defaultReferer,
		apiBase:               defaultAPI,
		pr:                    defaultPR,
		useTranscodingAddress: c.UseTranscodingAddress,
		onCookieUpdate:        c.OnCookieUpdate,
	}
	d.client = resty.New().
		SetTimeout(30*time.Second).
		SetHeader("Accept", "application/json, text/plain, */*").
		SetHeader("Referer", d.referer).
		SetHeader("User-Agent", d.ua)
	return d
}

func (d *Driver) Kind() string   { return "quark" }
func (d *Driver) ID() string     { return d.id }
func (d *Driver) RootID() string { return d.rootID }

// ---------- 公共请求 ----------

type resp struct {
	Status  int    `json:"status"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (d *Driver) request(ctx context.Context, path, method string, query map[string]string, body any, out any) error {
	req := d.client.R().
		SetContext(ctx).
		SetHeader("Cookie", d.cookie).
		SetQueryParam("pr", d.pr).
		SetQueryParam("fr", "pc")
	if query != nil {
		req.SetQueryParams(query)
	}
	if body != nil {
		req.SetBody(body)
	}
	if out != nil {
		req.SetResult(out)
	}
	var e resp
	req.SetError(&e)

	res, err := req.Execute(method, d.apiBase+path)
	if err != nil {
		return err
	}

	// 处理 cookie 刷新（__puus）
	for _, ck := range res.Cookies() {
		if ck.Name == "__puus" {
			d.cookie = setCookieValue(d.cookie, "__puus", ck.Value)
			if d.onCookieUpdate != nil {
				d.onCookieUpdate(d.cookie)
			}
		}
	}

	if e.Status >= 400 || e.Code != 0 {
		if e.Message == "" {
			return fmt.Errorf("quark api error: status=%d code=%d", e.Status, e.Code)
		}
		return errors.New(e.Message)
	}
	return nil
}

func (d *Driver) Init(ctx context.Context) error {
	return d.request(ctx, "/config", http.MethodGet, nil, nil, nil)
}

// ---------- 列目录 ----------

type file struct {
	Fid       string `json:"fid"`
	FileName  string `json:"file_name"`
	Size      int64  `json:"size"`
	Category  int    `json:"category"`
	File      bool   `json:"file"`
	UpdatedAt int64  `json:"updated_at"`
}

type sortResp struct {
	Data struct {
		List []file `json:"list"`
	} `json:"data"`
	Metadata struct {
		Total int `json:"_total"`
	} `json:"metadata"`
}

func (d *Driver) List(ctx context.Context, dirID string) ([]drives.Entry, error) {
	var out []drives.Entry
	page := 1
	size := 100
	for {
		q := map[string]string{
			"pdir_fid":             dirID,
			"_size":                strconv.Itoa(size),
			"_page":                strconv.Itoa(page),
			"_fetch_total":         "1",
			"fetch_all_file":       "1",
			"fetch_risk_file_name": "1",
		}
		var r sortResp
		if err := d.request(ctx, "/file/sort", http.MethodGet, q, nil, &r); err != nil {
			return nil, fmt.Errorf("quark list: %w", err)
		}
		for _, f := range r.Data.List {
			out = append(out, fileToEntry(&f, dirID))
		}
		if page*size >= r.Metadata.Total {
			break
		}
		page++
	}
	return out, nil
}

func (d *Driver) Stat(ctx context.Context, fileID string) (*drives.Entry, error) {
	// 夸克没提供单文件查询接口，回退到父目录遍历需要额外信息
	return nil, drives.ErrNotSupported
}

// ---------- 下载直链 ----------

type downResp struct {
	Data []struct {
		DownloadUrl string `json:"download_url"`
	} `json:"data"`
}

func (d *Driver) StreamURL(ctx context.Context, fileID string) (*drives.StreamLink, error) {
	body := map[string]any{"fids": []string{fileID}}
	var r downResp
	if err := d.request(ctx, "/file/download", http.MethodPost, nil, body, &r); err != nil {
		return nil, fmt.Errorf("quark download: %w", err)
	}
	if len(r.Data) == 0 || r.Data[0].DownloadUrl == "" {
		return nil, errors.New("quark download: empty url")
	}

	headers := http.Header{}
	headers.Set("User-Agent", d.ua)
	headers.Set("Referer", d.referer)
	headers.Set("Cookie", d.cookie)

	return &drives.StreamLink{
		URL:     r.Data[0].DownloadUrl,
		Headers: headers,
		Expires: time.Now().Add(10 * time.Minute),
	}, nil
}

// ---------- 创建目录 ----------

type mkdirResp struct {
	Data struct {
		Fid string `json:"fid"`
	} `json:"data"`
}

func (d *Driver) MakeDir(ctx context.Context, parentID, name string) (string, error) {
	body := map[string]any{
		"dir_init_lock": false,
		"dir_path":      "",
		"file_name":     name,
		"pdir_fid":      parentID,
	}
	var r mkdirResp
	if err := d.request(ctx, "/file", http.MethodPost, nil, body, &r); err != nil {
		return "", fmt.Errorf("quark mkdir: %w", err)
	}
	return r.Data.Fid, nil
}

func (d *Driver) EnsureDir(ctx context.Context, pathFromRoot string) (string, error) {
	parts := splitPath(pathFromRoot)
	currentID := d.rootID
	for _, name := range parts {
		childID, err := d.findChildDir(ctx, currentID, name)
		if err != nil {
			return "", err
		}
		if childID == "" {
			id, err := d.MakeDir(ctx, currentID, name)
			if err != nil {
				return "", err
			}
			childID = id
		}
		currentID = childID
	}
	return currentID, nil
}

func (d *Driver) findChildDir(ctx context.Context, parent, name string) (string, error) {
	entries, err := d.List(ctx, parent)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir && e.Name == name {
			return e.ID, nil
		}
	}
	return "", nil
}

// ---------- 上传（第一版不实现，走本地预览视频兜底） ----------

func (d *Driver) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	return "", drives.ErrNotSupported
}

// ---------- helpers ----------

func fileToEntry(f *file, parentID string) drives.Entry {
	return drives.Entry{
		ID:       f.Fid,
		Name:     f.FileName,
		Size:     f.Size,
		IsDir:    !f.File,
		ParentID: parentID,
		MimeType: guessMime(f.FileName),
		ModTime:  time.UnixMilli(f.UpdatedAt),
		Category: f.Category,
	}
}

func guessMime(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	}
	return "application/octet-stream"
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// setCookieValue 替换 cookie 字符串中某个 key 的值，不存在则追加
func setCookieValue(cookie, key, value string) string {
	if cookie == "" {
		return key + "=" + value
	}
	parts := strings.Split(cookie, ";")
	var out []string
	found := false
	for _, p := range parts {
		kv := strings.TrimSpace(p)
		if kv == "" {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if kv[:eq] == key {
			out = append(out, key+"="+value)
			found = true
		} else {
			out = append(out, kv)
		}
	}
	if !found {
		out = append(out, key+"="+value)
	}
	return strings.Join(out, "; ")
}

var _ drives.Drive = (*Driver)(nil)
