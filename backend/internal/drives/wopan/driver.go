package wopan

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	sdk "github.com/OpenListTeam/wopan-sdk-go"
	"github.com/video-site/backend/internal/drives"
)

// Driver 封装联通沃盘
type Driver struct {
	id            string
	rootID        string
	familyID      string
	accessToken   string
	refreshToken  string
	client        *sdk.WoClient
	onTokenUpdate func(access, refresh string)
}

type Config struct {
	ID           string
	AccessToken  string
	RefreshToken string
	FamilyID     string // 空则走个人空间，有值则走家庭空间
	RootID       string // 根目录 ID，默认 "0"
	// 当 SDK 刷新 token 时回调，便于持久化
	OnTokenUpdate func(access, refresh string)
}

func New(c Config) *Driver {
	rootID := c.RootID
	if rootID == "" {
		rootID = "0"
	}
	return &Driver{
		id:            c.ID,
		rootID:        rootID,
		familyID:      c.FamilyID,
		accessToken:   c.AccessToken,
		refreshToken:  c.RefreshToken,
		onTokenUpdate: c.OnTokenUpdate,
	}
}

func (d *Driver) Kind() string { return "wopan" }
func (d *Driver) ID() string   { return d.id }
func (d *Driver) RootID() string {
	return d.rootID
}

func (d *Driver) Init(ctx context.Context) error {
	d.client = sdk.DefaultWithRefreshToken(d.refreshToken)
	d.client.SetAccessToken(d.accessToken)
	d.client.OnRefreshToken(func(access, refresh string) {
		d.accessToken = access
		d.refreshToken = refresh
		if d.onTokenUpdate != nil {
			d.onTokenUpdate(access, refresh)
		}
	})
	// InitData 会触发一次 token 校验
	return d.client.InitData()
}

func (d *Driver) spaceType() string {
	if d.familyID != "" {
		return sdk.SpaceTypeFamily
	}
	return sdk.SpaceTypePersonal
}

func (d *Driver) List(ctx context.Context, dirID string) ([]drives.Entry, error) {
	var result []drives.Entry
	pageNum := 0
	pageSize := 100
	for {
		data, err := d.client.QueryAllFiles(d.spaceType(), dirID, pageNum, pageSize, 0, d.familyID)
		if err != nil {
			return nil, fmt.Errorf("wopan list: %w", err)
		}
		for _, f := range data.Files {
			result = append(result, fileToEntry(f, dirID))
		}
		if len(data.Files) < pageSize {
			break
		}
		pageNum++
	}
	return result, nil
}

func (d *Driver) Stat(ctx context.Context, fileID string) (*drives.Entry, error) {
	// 沃盘 SDK 没有单文件查询，退化为遍历父目录 —— 这里第一版只在 scanner 路径使用 List，Stat 保留 stub
	return nil, drives.ErrNotSupported
}

func (d *Driver) StreamURL(ctx context.Context, fileID string) (*drives.StreamLink, error) {
	data, err := d.client.GetDownloadUrlV2([]string{fileID})
	if err != nil {
		return nil, fmt.Errorf("wopan download url: %w", err)
	}
	if len(data.List) == 0 {
		return nil, fmt.Errorf("wopan download url: empty response")
	}
	return &drives.StreamLink{
		URL:     data.List[0].DownloadUrl,
		Headers: http.Header{},
		Expires: time.Now().Add(10 * time.Minute),
	}, nil
}

func (d *Driver) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	// wopan SDK 要求 *os.File，先把流落到临时文件再上传
	tmp, err := os.CreateTemp("", "wopan-upload-*.tmp")
	if err != nil {
		return "", err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()
	if _, err := io.Copy(tmp, r); err != nil {
		return "", err
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		return "", err
	}
	fid, err := d.client.Upload2C(d.spaceType(), sdk.Upload2CFile{
		Name:        name,
		Size:        size,
		Content:     tmp,
		ContentType: "application/octet-stream",
	}, parentID, d.familyID, sdk.Upload2COption{Ctx: ctx})
	if err != nil {
		return "", fmt.Errorf("wopan upload: %w", err)
	}
	return fid, nil
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
			resp, err := d.client.CreateDirectory(d.spaceType(), currentID, name, d.familyID)
			if err != nil {
				return "", fmt.Errorf("wopan mkdir %s: %w", name, err)
			}
			childID = resp.Id
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

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func fileToEntry(f *sdk.File, parentID string) drives.Entry {
	mod, _ := time.Parse("2006-01-02 15:04:05", f.CreateTime)
	name := f.Name
	isDir := f.Type == 0
	id := f.Fid
	if id == "" {
		id = f.Id
	}
	if isDir && !strings.HasSuffix(name, "/") {
		// 不改 name，只标志
	}
	return drives.Entry{
		ID:       id,
		Name:     name,
		Size:     f.Size,
		IsDir:    isDir,
		ParentID: parentID,
		MimeType: guessMime(name),
		ModTime:  mod,
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

// 确保实现接口
var _ drives.Drive = (*Driver)(nil)
