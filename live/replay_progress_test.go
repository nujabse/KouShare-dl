package live

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplayProgressOutput_LocalHLS(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found; skipping")
	}

	tmp := t.TempDir()
	inputMP4 := filepath.Join(tmp, "input.mp4")
	playlist := filepath.Join(tmp, "playlist.m3u8")
	segmentPattern := filepath.Join(tmp, "seg%03d.ts")
	outputMP4 := filepath.Join(tmp, "out.mp4")

	// Generate a tiny MP4.
	cmd := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", "testsrc=duration=3:size=320x240:rate=30",
		"-f", "lavfi",
		"-i", "sine=frequency=1000:duration=3",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		inputMP4,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg generate input failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// Convert to HLS VOD.
	cmd = exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputMP4,
		"-hls_time", "1",
		"-hls_list_size", "0",
		"-hls_segment_filename", segmentPattern,
		playlist,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg hls failed: %v: %s", err, strings.TrimSpace(string(out)))
	}

	// Capture stdout to verify we don't spam logs.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	if err := downloadHLSWithFFmpeg(playlist, outputMP4); err != nil {
		_ = w.Close()
		_ = r.Close()
		t.Fatalf("downloadHLSWithFFmpeg failed: %v", err)
	}
	_ = w.Close()
	_ = r.Close()

	outBytes := <-done
	outStr := string(outBytes)

	if !strings.Contains(outStr, "开始下载（显示总进度与速度）") {
		t.Fatalf("expected progress start message, got: %q", outStr)
	}
	if !strings.Contains(outStr, "进度:") || !strings.Contains(outStr, "速度:") {
		t.Fatalf("expected percent+speed output, got: %q", outStr)
	}
	if strings.Contains(outStr, "Opening '") {
		t.Fatalf("unexpected verbose ffmpeg output leaked to stdout: %q", outStr)
	}
	// Ensure output file exists.
	if st, err := os.Stat(outputMP4); err != nil || st.Size() == 0 {
		t.Fatalf("output mp4 missing/empty: %v size=%d", err, func() int64 { if st == nil { return 0 }; return st.Size() }())
	}
}
