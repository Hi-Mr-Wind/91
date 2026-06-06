package pikpak

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/go-resty/resty/v2"
)

// PikPak 上传协议（参考 OpenList drivers/pikpak）：
//
//   1. POST https://api-drive.mypikpak.net/drive/v1/files
//      body: {kind, name, size, hash(GCID), upload_type=UPLOAD_TYPE_RESUMABLE,
//             objProvider, parent_id, folder_type}
//
//   2. 服务端响应 uploadTaskData：
//      - 命中秒传：resumable=null，file.id 就是新文件 ID（无需上传字节）
//      - 未命中：resumable.params 含 S3 兼容凭证（access_key / secret /
//        bucket / endpoint / key / security_token）
//
//   3. 用 Aliyun OSS SDK PutObject 把字节传到 endpoint+bucket+key
//
//   4. PikPak 服务端轮询 OSS，发现完成后把 resp.File.ID 标记为可用；
//      所以 Upload 完成后直接返回 resp.File.ID 即可（一开始就有，
//      只是文件实体未就绪）。

const (
	ossSecurityTokenHeaderName = "X-OSS-Security-Token"
	ossUserAgent               = "aliyun-sdk-android/2.9.13(Linux/Android 14/M2004j7ac;UKQ1.231108.001)"
	// 单次 PutObject 的硬上限（OSS 文档限制 5GiB；保守用 5GiB-1）。
	// spider91 视频通常 ~100MiB，远低于该值。超过则需走 multipart，
	// 当前未实现，遇到会显式报错。
	maxSinglePutSize = 5*1024*1024*1024 - 1
)

// uploadTaskData 是 POST /drive/v1/files 的响应结构。
type uploadTaskData struct {
	UploadType string         `json:"upload_type"`
	Resumable  *resumableData `json:"resumable"`
	File       file           `json:"file"`
}

type resumableData struct {
	Kind     string   `json:"kind"`
	Params   s3Params `json:"params"`
	Provider string   `json:"provider"`
}

type s3Params struct {
	AccessKeyID     string    `json:"access_key_id"`
	AccessKeySecret string    `json:"access_key_secret"`
	Bucket          string    `json:"bucket"`
	Endpoint        string    `json:"endpoint"`
	Expiration      time.Time `json:"expiration"`
	Key             string    `json:"key"`
	SecurityToken   string    `json:"security_token"`
}

// UploadResult 是 UploadAndReportHash 的返回值。
// FileID 是 PikPak 分配的新文件 ID；Hash 是本次上传的 GCID（HEX 大写）；
// Size 是实际写入的字节数（与传入的 size 应一致）。
type UploadResult struct {
	FileID string
	Hash   string
	Size   int64
}

// Upload 实现 drives.Drive 接口；只返回 fileID。
// 完整上传元数据见 UploadAndReportHash。
func (d *Driver) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	res, err := d.UploadAndReportHash(ctx, parentID, name, r, size)
	if err != nil {
		return "", err
	}
	return res.FileID, nil
}

// UploadAndReportHash 上传并返回 file ID + GCID + 实际字节数。
//
// 用于 spider91 → PikPak 迁移 worker：上传完后直接把 hash 写回 catalog
// 的 content_hash 字段，避免再读一次本地文件做 hash。
//
// 参数：
//   - parentID：PikPak 目录 fileID。空字符串或 "/" 时回退到 driver 自身的 rootID。
//   - name：上传后的文件名（含扩展名）。
//   - r：字节流。会被先全量缓冲到临时文件以便算 GCID + 重试。
//   - size：流的总字节数。必须准确（PikPak API 要求 size 字段）。
//
// 实现要点：
//   - 必须先算 GCID 再申请上传会话（PikPak API 要求 hash 字段），
//     所以这里先 io.Copy 到临时文件并同步算 GCID。
//   - 命中秒传时不发任何字节；否则用 OSS PutObject 上传。
//   - 单次 PutObject 上限保守用 5GiB-1。spider91 视频远小于此值，
//     超出该值会报错（暂不实现 multipart）。
func (d *Driver) UploadAndReportHash(ctx context.Context, parentID, name string, r io.Reader, size int64) (UploadResult, error) {
	if r == nil {
		return UploadResult{}, errors.New("pikpak upload: nil reader")
	}
	if size < 0 {
		return UploadResult{}, fmt.Errorf("pikpak upload: invalid size %d", size)
	}
	if size > maxSinglePutSize {
		return UploadResult{}, fmt.Errorf("pikpak upload: file size %d exceeds %d (multipart not implemented)", size, maxSinglePutSize)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return UploadResult{}, errors.New("pikpak upload: empty file name")
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" || parentID == "/" {
		parentID = d.rootID
	}

	// 1) 把 r 全量缓冲到临时文件，同时算 GCID。
	tmp, gcidHex, actualSize, err := bufferAndHashGCID(r, size)
	if err != nil {
		return UploadResult{}, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	// 2) 申请上传会话。
	var resp uploadTaskData
	if err := d.request(ctx, filesURL, http.MethodPost, func(req *resty.Request) {
		req.SetBody(map[string]any{
			"kind":        "drive#file",
			"name":        name,
			"size":        actualSize,
			"hash":        gcidHex,
			"upload_type": "UPLOAD_TYPE_RESUMABLE",
			"objProvider": map[string]any{"provider": "UPLOAD_TYPE_UNKNOWN"},
			"parent_id":   parentID,
			"folder_type": "NORMAL",
		})
	}, &resp); err != nil {
		return UploadResult{}, fmt.Errorf("pikpak upload: request session: %w", err)
	}

	result := UploadResult{Hash: gcidHex, Size: actualSize}

	// 3) 命中秒传：服务端已经知道这个 hash，直接返回新文件 ID。
	if resp.Resumable == nil {
		if resp.File.ID != "" {
			result.FileID = resp.File.ID
			return result, nil
		}
		// 极少数情况下 file.id 不在响应里，回退到列父目录找名字。
		fid, err := d.findFileIDByName(ctx, parentID, name)
		if err != nil {
			return UploadResult{}, err
		}
		result.FileID = fid
		return result, nil
	}

	// 4) 未命中秒传：把字节传到 S3 兼容存储。
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return UploadResult{}, fmt.Errorf("pikpak upload: seek tmp: %w", err)
	}
	if err := d.uploadToOSS(ctx, &resp.Resumable.Params, tmp); err != nil {
		return UploadResult{}, fmt.Errorf("pikpak upload: oss put: %w", err)
	}

	// 5) 拿到 fileID。优先走响应里的预分配 ID；为空就回查目录。
	if resp.File.ID != "" {
		result.FileID = resp.File.ID
		return result, nil
	}
	fid, err := d.findFileIDByName(ctx, parentID, name)
	if err != nil {
		return UploadResult{}, err
	}
	result.FileID = fid
	return result, nil
}

// bufferAndHashGCID 把 r 复制到一个临时文件，同时计算 GCID。
// 返回临时文件（位置在末尾，需要调用方 Seek 回 0）、GCID hex 大写、实际写入字节数。
//
// 调用方负责 Close + Remove 临时文件。
func bufferAndHashGCID(r io.Reader, size int64) (*os.File, string, int64, error) {
	tmp, err := os.CreateTemp("", "pikpak-upload-*.bin")
	if err != nil {
		return nil, "", 0, fmt.Errorf("pikpak upload: create tmp: %w", err)
	}

	h := NewGCID(size)
	mw := io.MultiWriter(tmp, h)
	written, err := io.Copy(mw, r)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", 0, fmt.Errorf("pikpak upload: buffer body: %w", err)
	}
	if size > 0 && written != size {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, "", 0, fmt.Errorf("pikpak upload: size mismatch: declared %d, copied %d", size, written)
	}
	gcidHex := strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
	return tmp, gcidHex, written, nil
}

// uploadToOSS 用 Aliyun OSS SDK 把 body 全量 PutObject 到 PikPak 提供的 S3 端点。
//
// 参数复用 PikPak 的临时凭证；必须带 Security Token 头部 + UserAgent，与 OpenList 一致。
func (d *Driver) uploadToOSS(ctx context.Context, p *s3Params, body io.Reader) error {
	if p == nil {
		return errors.New("pikpak upload: nil s3 params")
	}
	client, err := oss.New(p.Endpoint, p.AccessKeyID, p.AccessKeySecret)
	if err != nil {
		return fmt.Errorf("oss client: %w", err)
	}
	bucket, err := client.Bucket(p.Bucket)
	if err != nil {
		return fmt.Errorf("oss bucket: %w", err)
	}
	// OSS SDK 不接受 context 取消；我们用 readerWithCtx 把 ctx 织入读链路，
	// ctx 取消时下次 Read 会返回错误，OSS PutObject 会随之中断。
	wrapped := &readerWithCtx{ctx: ctx, r: body}
	return bucket.PutObject(p.Key, wrapped,
		oss.SetHeader(ossSecurityTokenHeaderName, p.SecurityToken),
		oss.UserAgentHeader(ossUserAgent),
	)
}

type readerWithCtx struct {
	ctx context.Context
	r   io.Reader
}

func (rc *readerWithCtx) Read(p []byte) (int, error) {
	if err := rc.ctx.Err(); err != nil {
		return 0, err
	}
	return rc.r.Read(p)
}

// findFileIDByName 列出 parentID 目录，返回名字完全匹配 name 的第一个文件的 ID。
// 用于秒传或上传后兜底取 fileID 的情况；带短暂重试以等待服务端持久化。
func (d *Driver) findFileIDByName(ctx context.Context, parentID, name string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		files, err := d.getFiles(ctx, parentID)
		if err != nil {
			lastErr = err
			continue
		}
		for _, f := range files {
			if f.Name == name && f.Kind != "drive#folder" {
				return f.ID, nil
			}
		}
		lastErr = fmt.Errorf("uploaded file %q not found in parent %q", name, parentID)
	}
	return "", lastErr
}
