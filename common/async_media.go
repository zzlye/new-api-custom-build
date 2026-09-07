package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const AsyncMediaRetentionOption = "AsyncMediaRetentionHours"
const AsyncMediaConcurrencyOption = "AsyncMediaConcurrency"
const AsyncMediaMaxFileBytes int64 = 512 * 1024 * 1024

// AsyncMediaConcurrency 每次领取任务时读取设置；零表示不增加额外并发上限，旧配置默认四个。
func AsyncMediaConcurrency() int {
	OptionMapRWMutex.RLock()
	value := OptionMap[AsyncMediaConcurrencyOption]
	OptionMapRWMutex.RUnlock()
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 || limit > 256 {
		return 4
	}
	return limit
}

// AsyncMediaRetentionSeconds 每次读取根用户设置，使修改对尚未清理的结果立即生效。
func AsyncMediaRetentionSeconds() int64 {
	OptionMapRWMutex.RLock()
	value := OptionMap[AsyncMediaRetentionOption]
	OptionMapRWMutex.RUnlock()
	hours, err := strconv.Atoi(value)
	if err != nil || hours < 1 || hours > 168 {
		hours = 2
	}
	return int64(hours) * 3600
}

// AsyncMediaDir 使用独立的持久目录，避免任务请求被普通临时缓存清理器误删。
func AsyncMediaDir() string {
	dir := os.Getenv("ASYNC_MEDIA_DIR")
	if dir == "" {
		dir = filepath.Join("data", "async-media")
	}
	absolute, err := filepath.Abs(dir)
	if err == nil {
		return absolute
	}
	return dir
}

// CreateAsyncMediaFile 以仅服务进程可读写的权限保存请求或生成结果。
func CreateAsyncMediaFile() (string, *os.File, error) {
	dir := AsyncMediaDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp(dir, "media-*")
	if err != nil {
		return "", nil, err
	}
	return file.Name(), file, nil
}

// RemoveAsyncMediaFile 只删除媒体目录内的单个文件，同时兼容迁移前的任务缓存。
func RemoveAsyncMediaFile(path string) error {
	if path == "" {
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, dir := range []string{AsyncMediaDir(), GetDiskCacheDir()} {
		base, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(base, absolute)
		if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative) {
			info, statErr := os.Lstat(absolute)
			if os.IsNotExist(statErr) {
				return nil
			}
			if statErr != nil {
				return statErr
			}
			if info.IsDir() {
				return fmt.Errorf("媒体清理目标应为文件")
			}
			return os.Remove(absolute)
		}
	}
	return fmt.Errorf("媒体文件路径超出保存目录")
}
