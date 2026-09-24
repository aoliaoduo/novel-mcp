package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

// appendLog 管理只增长事实的 JSONL 存储。调用方负责持有 io.mu 写锁。
// 首次加载建立内存去重索引；正常追加只写新增记录。
type appendLog[T any] struct {
	path  string
	key   func(T) string
	clone func(T) T

	loaded    bool
	hasLog    bool
	tailBytes int
	values    []T
	seen      map[string]struct{}
}

func newAppendLog[T any](path string, key func(T) string, clone func(T) T) *appendLog[T] {
	return &appendLog[T]{path: path, key: key, clone: clone}
}

func (l *appendLog[T]) loadUnlocked(io *IO) error {
	if l.loaded {
		return nil
	}
	l.reset()

	data, tailBytes, err := committedJSONLinesUnlocked(io, l.path)
	switch {
	case err == nil:
		values, err := decodeJSONLines[T](l.path, data)
		if err != nil {
			return err
		}
		l.hasLog = true
		l.tailBytes = tailBytes
		l.setValues(values)
	case os.IsNotExist(err):
		l.setValues(nil)
	default:
		return err
	}
	l.loaded = true
	return nil
}

func (l *appendLog[T]) allUnlocked(io *IO) ([]T, error) {
	if err := l.loadUnlocked(io); err != nil {
		return nil, err
	}
	return l.cloneValues(l.values), nil
}

// appendUnlocked 返回实际新增的记录。
func (l *appendLog[T]) appendUnlocked(io *IO, incoming []T) ([]T, error) {
	if err := l.loadUnlocked(io); err != nil {
		return nil, err
	}
	if l.tailBytes > 0 {
		if err := l.repairTailUnlocked(io); err != nil {
			l.reset()
			return nil, err
		}
	}

	added := make([]T, 0, len(incoming))
	pending := make(map[string]struct{}, len(incoming))
	for _, value := range incoming {
		key := l.key(value)
		if _, ok := l.seen[key]; ok {
			continue
		}
		if _, ok := pending[key]; ok {
			continue
		}
		pending[key] = struct{}{}
		added = append(added, l.clone(value))
	}

	if !l.hasLog && len(added) > 0 {
		data, err := encodeJSONLines(added)
		if err != nil {
			return nil, err
		}
		if err := io.WriteFileUnlocked(l.path, data); err != nil {
			l.reset()
			return nil, err
		}
		l.hasLog = true
	} else if len(added) > 0 {
		data, err := encodeJSONLines(added)
		if err != nil {
			return nil, err
		}
		if err := io.AppendLineUnlocked(l.path, data); err != nil {
			// 写入可能留下未换行的尾部。丢弃缓存，让下一次加载按提交协议
			// 显式截断未提交尾部后再重放。
			l.reset()
			return nil, err
		}
	} else if len(incoming) > 0 && l.hasLog {
		// 上一次追加可能已写完整记录，但 Sync 返回了错误。
		// 幂等重放在确认成功前再次同步，不把“当前可读”误当成“已持久化”。
		if err := io.syncFileUnlocked(l.path); err != nil {
			l.reset()
			return nil, err
		}
	}

	for _, value := range added {
		cloned := l.clone(value)
		l.values = append(l.values, cloned)
		l.seen[l.key(cloned)] = struct{}{}
	}

	return l.cloneValues(added), nil
}

func (l *appendLog[T]) replaceUnlocked(io *IO, values []T) error {
	data, err := encodeJSONLines(values)
	if err != nil {
		return err
	}
	if err := io.WriteFileUnlocked(l.path, data); err != nil {
		l.reset()
		return err
	}
	l.hasLog = true
	l.tailBytes = 0
	l.setValues(values)
	l.loaded = true
	return nil
}

func (l *appendLog[T]) setValues(values []T) {
	l.values = l.cloneValues(values)
	l.seen = make(map[string]struct{}, len(values))
	for _, value := range l.values {
		l.seen[l.key(value)] = struct{}{}
	}
}

func (l *appendLog[T]) cloneValues(values []T) []T {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]T, len(values))
	for i, value := range values {
		cloned[i] = l.clone(value)
	}
	return cloned
}

func (l *appendLog[T]) reset() {
	l.loaded = false
	l.hasLog = false
	l.tailBytes = 0
	l.values = nil
	l.seen = nil
}

func (l *appendLog[T]) repairTailUnlocked(io *IO) error {
	if l.tailBytes <= 0 {
		return nil
	}
	info, err := os.Stat(io.path(l.path))
	if err != nil {
		return err
	}
	keep := info.Size() - int64(l.tailBytes)
	if keep < 0 {
		return fmt.Errorf("invalid uncommitted tail size for %s", l.path)
	}
	if err := os.Truncate(io.path(l.path), keep); err != nil {
		return err
	}
	if err := io.syncFileUnlocked(l.path); err != nil {
		return err
	}
	slog.Warn("已在写入前丢弃追加日志的未提交尾部",
		"module", "store", "file", l.path, "discarded_bytes", l.tailBytes)
	l.tailBytes = 0
	return nil
}

func encodeJSONLines[T any](values []T) ([]byte, error) {
	var data bytes.Buffer
	for i, value := range values {
		line, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode jsonl record %d: %w", i+1, err)
		}
		data.Write(line)
		data.WriteByte('\n')
	}
	return data.Bytes(), nil
}

func decodeJSONLines[T any](path string, data []byte) ([]T, error) {
	lines := bytes.Split(data, []byte{'\n'})
	values := make([]T, 0, len(lines))
	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var value T
		if err := json.Unmarshal(line, &value); err != nil {
			return nil, fmt.Errorf("parse %s line %d: %w", path, i+1, err)
		}
		values = append(values, value)
	}
	return values, nil
}

// committedJSONLinesUnlocked 只读取协议上已提交的前缀：只有以换行结束的 JSONL
// 记录才算提交。读路径不修磁盘；未提交尾部由下一次 append 写入前截断。
// 完整行损坏仍严格报错，不做猜测式修复。
func committedJSONLinesUnlocked(io *IO, path string) ([]byte, int, error) {
	data, err := io.ReadFileUnlocked(path)
	if err != nil {
		return nil, 0, err
	}
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return data, 0, nil
	}
	keep := bytes.LastIndexByte(data, '\n') + 1
	return data[:keep], len(data) - keep, nil
}
