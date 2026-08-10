package appearance_setting

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	BackgroundVideoWidth  = 1920
	BackgroundVideoHeight = 1080
	BackgroundVideoFPS    = 30

	backgroundVideoOptimizationTimeout = 10 * time.Minute
	backgroundVideoErrorOutputLimit    = 800
)

type limitedTailWriter struct {
	data  []byte
	limit int
}

// Write 仅保留最新的错误输出，避免异常媒体让 FFmpeg 日志持续占用内存。
func (writer *limitedTailWriter) Write(content []byte) (int, error) {
	written := len(content)
	if writer.limit <= 0 {
		return written, nil
	}
	if len(content) >= writer.limit {
		writer.data = append(writer.data[:0], content[len(content)-writer.limit:]...)
		return written, nil
	}
	if overflow := len(writer.data) + len(content) - writer.limit; overflow > 0 {
		copy(writer.data, writer.data[overflow:])
		writer.data = writer.data[:len(writer.data)-overflow]
	}
	writer.data = append(writer.data, content...)
	return written, nil
}

func (writer *limitedTailWriter) String() string {
	return string(writer.data)
}

// OptimizeBackgroundVideo 将上传视频转换为适合网页背景播放的固定规格。
func OptimizeBackgroundVideo(ctx context.Context, sourcePath, outputPath string) (int64, error) {
	ffmpegPath, err := findFFmpeg()
	if err != nil {
		return 0, err
	}

	optimizeContext, cancel := context.WithTimeout(ctx, backgroundVideoOptimizationTimeout)
	defer cancel()

	command := exec.CommandContext(
		optimizeContext,
		ffmpegPath,
		backgroundVideoFFmpegArgs(sourcePath, outputPath)...,
	)
	errorOutput := limitedTailWriter{limit: backgroundVideoErrorOutputLimit}
	command.Stderr = &errorOutput
	err = command.Run()
	if err != nil {
		if errors.Is(optimizeContext.Err(), context.DeadlineExceeded) {
			return 0, fmt.Errorf("视频优化超时: %w", optimizeContext.Err())
		}
		message := strings.TrimSpace(errorOutput.String())
		if message == "" {
			return 0, fmt.Errorf("视频优化失败: %w", err)
		}
		return 0, fmt.Errorf("视频优化失败: %w: %s", err, message)
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return 0, fmt.Errorf("读取优化视频失败: %w", err)
	}
	if info.Size() <= 0 {
		return 0, fmt.Errorf("优化视频为空")
	}
	return info.Size(), nil
}

func findFFmpeg() (string, error) {
	if configuredPath := strings.TrimSpace(os.Getenv("FFMPEG_PATH")); configuredPath != "" {
		if info, err := os.Stat(configuredPath); err == nil && !info.IsDir() {
			return configuredPath, nil
		}
		return "", fmt.Errorf("FFMPEG_PATH 指向的文件不存在")
	}

	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("未找到 FFmpeg，请设置 FFMPEG_PATH: %w", err)
	}
	return path, nil
}

func backgroundVideoFFmpegArgs(sourcePath, outputPath string) []string {
	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,fps=%d,setsar=1",
		BackgroundVideoWidth,
		BackgroundVideoHeight,
		BackgroundVideoWidth,
		BackgroundVideoHeight,
		BackgroundVideoFPS,
	)

	return []string{
		"-hide_banner",
		"-nostdin",
		"-loglevel", "error",
		"-y",
		"-i", sourcePath,
		"-map", "0:v:0",
		"-vf", filter,
		"-an",
		"-sn",
		"-dn",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "24",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		"-map_metadata", "-1",
		outputPath,
	}
}
