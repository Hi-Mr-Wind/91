package pikpak

import (
	"crypto/sha1"
	"encoding"
	"hash"
)

// GCID 是 PikPak 用于 hash 校验和秒传的自定义算法。
//
// 算法（来自 OpenList pkg/utils/hash/gcid.go）：
//   - 把整个文件按 blockSize 分块；
//   - 每块算 SHA1，拼成中间序列；
//   - 对中间序列再算 SHA1，得到最终 hash。
//
// blockSize 由文件大小决定：从 0x40000 (256KiB) 开始翻倍，直到
// (size / blockSize) <= 512 或 blockSize 达到上限 0x200000 (2MiB)。
//
// 输出是十六进制大写，长度恒为 40 字符（标准 SHA1 字段宽度）。
//
// 用法：
//
//	h := NewGCID(size)        // size 是即将上传的文件总字节数
//	io.Copy(h, file)
//	digest := h.Sum(nil)      // 20 字节 binary
//	hex := strings.ToUpper(hex.EncodeToString(digest))
func NewGCID(size int64) hash.Hash {
	return &gcid{
		hash:      sha1.New(),
		hashState: sha1.New(),
		blockSize: int(gcidBlockSize(size)),
	}
}

func gcidBlockSize(size int64) int64 {
	var psize int64 = 0x40000
	for size > 0 && float64(size)/float64(psize) > 0x200 && psize < 0x200000 {
		psize <<= 1
	}
	return psize
}

type gcid struct {
	hash      hash.Hash // 累计：对每一块的 SHA1 再求 SHA1
	hashState hash.Hash // 当前块的 SHA1 状态
	blockSize int

	offset int // 当前块已写入字节
}

func (h *gcid) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if h.offset < h.blockSize {
			rem := h.blockSize - h.offset
			if rem > len(p) {
				rem = len(p)
			}
			h.hashState.Write(p[:rem])
			h.offset += rem
			p = p[rem:]
		}
		if h.offset >= h.blockSize {
			h.hash.Write(h.hashState.Sum(nil))
			h.hashState.Reset()
			h.offset = 0
		}
	}
	return n, nil
}

// Sum 把当前未满一块的部分先 finalize 后再返回累积 hash，
// 但不破坏内部状态（多次 Sum 应该返回相同值）。
func (h *gcid) Sum(b []byte) []byte {
	if h.offset != 0 {
		// crypto/sha1 实现了 BinaryMarshaler/Unmarshaler，
		// 利用它把当前 outer hash 状态备份一下，写入 partial block 的 SHA1，
		// 算出 sum 再恢复，避免 Sum 之后还能继续 Write 的语义被破坏。
		if hm, ok := h.hash.(encoding.BinaryMarshaler); ok {
			if hum, ok := h.hash.(encoding.BinaryUnmarshaler); ok {
				snapshot, _ := hm.MarshalBinary()
				defer func() { _ = hum.UnmarshalBinary(snapshot) }()
				h.hash.Write(h.hashState.Sum(nil))
			}
		}
	}
	return h.hash.Sum(b)
}

func (h *gcid) Reset() {
	h.hash.Reset()
	h.hashState.Reset()
	h.offset = 0
}

func (h *gcid) Size() int { return h.hash.Size() }

func (h *gcid) BlockSize() int { return h.blockSize }
