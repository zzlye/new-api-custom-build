package appearance_setting

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptimizeBackgroundVideoProducesWebPlaybackProfile(t *testing.T) {
	ffmpegPath, err := resolveMediaToolForTest("ffmpeg")
	if err != nil {
		t.Skip("测试环境未安装 FFmpeg")
	}
	ffprobePath, err := resolveMediaToolForTest("ffprobe")
	if err != nil {
		t.Skip("测试环境未安装 FFprobe")
	}
	t.Setenv("FFMPEG_PATH", ffmpegPath)

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.mp4")
	outputPath := filepath.Join(tempDir, "optimized.mp4")

	// 构造带音轨、高帧率和非标准像素比例的输入，覆盖网页背景的关键优化结果。
	generate := exec.Command(
		ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=720x576:rate=60",
		"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=44100",
		"-t", "0.5", "-vf", "setsar=16/15",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac",
		sourcePath,
	)
	generateOutput, err := generate.CombinedOutput()
	require.NoErrorf(t, err, "生成测试视频失败: %s", generateOutput)

	size, err := OptimizeBackgroundVideo(t.Context(), sourcePath, outputPath)
	require.NoError(t, err)
	require.Positive(t, size)

	videoProbe := exec.Command(
		ffprobePath,
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,r_frame_rate,sample_aspect_ratio",
		"-of", "default=noprint_wrappers=1",
		outputPath,
	)
	videoOutput, err := videoProbe.CombinedOutput()
	require.NoErrorf(t, err, "读取优化视频信息失败: %s", videoOutput)
	videoInfo := string(videoOutput)
	require.Contains(t, videoInfo, "width=1920")
	require.Contains(t, videoInfo, "height=1080")
	require.Contains(t, videoInfo, "r_frame_rate=30/1")
	require.Contains(t, videoInfo, "sample_aspect_ratio=1:1")

	audioProbe := exec.Command(
		ffprobePath,
		"-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=index", "-of", "csv=p=0",
		outputPath,
	)
	audioOutput, err := audioProbe.CombinedOutput()
	require.NoErrorf(t, err, "读取优化视频音轨失败: %s", audioOutput)
	require.Empty(t, strings.TrimSpace(string(audioOutput)))
}

func TestFindFFmpegRejectsMissingConfiguredPath(t *testing.T) {
	t.Setenv("FFMPEG_PATH", "missing-ffmpeg-for-appearance-test")

	_, err := findFFmpeg()

	require.ErrorContains(t, err, "FFMPEG_PATH")
}

func TestLimitedTailWriterKeepsLatestOutput(t *testing.T) {
	writer := limitedTailWriter{limit: 5}

	written, err := writer.Write([]byte("abc"))
	require.NoError(t, err)
	require.Equal(t, 3, written)
	written, err = writer.Write([]byte("defg"))
	require.NoError(t, err)
	require.Equal(t, 4, written)
	require.Equal(t, "cdefg", writer.String())

	written, err = writer.Write([]byte("123456"))
	require.NoError(t, err)
	require.Equal(t, 6, written)
	require.Equal(t, "23456", writer.String())
}

// resolveMediaToolForTest 优先复用配置目录中的媒体工具，再查询系统路径。
func resolveMediaToolForTest(name string) (string, error) {
	if configuredPath := strings.TrimSpace(os.Getenv("FFMPEG_PATH")); configuredPath != "" {
		extension := filepath.Ext(configuredPath)
		candidate := filepath.Join(filepath.Dir(configuredPath), name+extension)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return exec.LookPath(name)
}
