package live

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/yliu7949/KouShare-dl/internal/color"
	"github.com/yliu7949/KouShare-dl/internal/config"
	"github.com/yliu7949/KouShare-dl/user"
)

// DownloadReplayVideo 下载指定直播间的快速回放视频
func (l *Live) DownloadReplayVideo() {
	if l.VideoID != "" {
		l.downloadReplayViaAPICore()
		return
	}

	if !l.getLidByRoomID() {
		return
	}
	l.checkLiveStatus()
	l.getLiveByRoomID(true)

	if l.isLive == "1" {
		fmt.Printf(`直播间正在直播中。可使用“ks record %s”命令录制该直播间。`, l.RoomID)
		return
	}

	// 直播回放有四种状态：直播结束不久回放尚未上线；已上线快速回放；已上线正式录播回放；本场直播无回放。
	switch l.isLive {
	case "0":
		fmt.Println("直播尚未开始，无快速回放。")
	case "2":
		if l.quickReplayURL != "" { //若有快速回放，则下载快速回放视频
			l.recordVOD()
		} else if l.playback == "0" {
			fmt.Println("本场直播无回放。")
		} else if l.playback == "1" {
			fmt.Println("快速回放暂未上线。")
		}
	case "3":
		fmt.Println("正式回放视频已上线。")
		if l.rtmpURL != "" {
			vid := strings.Split(l.rtmpURL, "/")[len(strings.Split(l.rtmpURL, "/"))-1]
			fmt.Printf("访问 %s 观看录播视频或使用“ks save %s”命令下载正式回放视频。\n", l.rtmpURL, vid)
		}
	default:
		fmt.Println("暂时无法下载回放视频。")
	}
}

func (l *Live) downloadReplayViaAPICore() {
	if l.SaveDir != "" {
		if err := os.MkdirAll(l.SaveDir, os.ModePerm); err != nil {
			fmt.Println("创建下载文件夹失败：", err)
			return
		}
	}

	l.tryPopulateLiveMetaFromAPICore()
	if strings.TrimSpace(l.title) != "" {
		fmt.Printf("回放标题：%s\n", color.Emphasize(l.title))
	}

	playbackURL := config.APIBaseURL() + "/live/v2/live/playback/" + l.RoomID + "?videoId=" + url.QueryEscape(l.VideoID)
	resp, err := user.MyRequest(http.MethodPost, playbackURL, []byte("{}"))
	if err != nil {
		fmt.Println("Get请求出错：", err)
		return
	}

	if gjson.Get(resp, "code").Int() != 200000 {
		msg := gjson.Get(resp, "msg").String()
		if msg == "" {
			msg = "请求失败"
		}
		fmt.Println(msg)
		return
	}

	bestURL := ""
	bestHeight := int64(0)
	for _, item := range gjson.Get(resp, "data.playbackUrls.0.list").Array() {
		fileURL := item.Get("fileUrl").String()
		if fileURL == "" {
			continue
		}
		height := item.Get("height").Int()
		if height > bestHeight {
			bestHeight = height
			bestURL = fileURL
		}
	}
	if bestURL == "" {
		fmt.Println("未获取到可下载的回放播放地址（可能需要登录/权限）。")
		return
	}

	titlePart := sanitizeFilePart(l.title)
	if titlePart != "" {
		titlePart += "_"
	}
	outputName := fmt.Sprintf("%sreplay_room%s_vid%s_%sp_%s.mp4",
		titlePart,
		l.RoomID,
		l.VideoID,
		strconv.FormatInt(bestHeight, 10),
		time.Now().Format("2006-01-02_15-04-05"),
	)
	outputPath := filepath.Join(l.SaveDir, outputName)

	fmt.Printf("清晰度：%sp\n", strconv.FormatInt(bestHeight, 10))
	if err := downloadHLSWithFFmpeg(bestURL, outputPath); err != nil {
		fmt.Println("ffmpeg 下载失败：", err)
		return
	}
	fmt.Println("快速回放视频下载完成：", outputPath)
}

func downloadHLSWithFFmpeg(m3u8URL string, outputPath string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("未找到 ffmpeg，请先安装 ffmpeg 或将其加入 PATH")
	}

	headers := "Referer: " + config.WebBaseURL() + "/\r\n" +
		"User-Agent: Mozilla/5.0\r\n"

	totalDurationSec := probeDurationSeconds(m3u8URL, headers)
	if totalDurationSec > 0 {
		fmt.Printf("总时长：%s\n", formatDurationSeconds(totalDurationSec))
	}
	fmt.Println("开始下载（显示总进度与速度）...")

	cmd := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-y",
		"-loglevel", "error",
		"-nostats",
		"-progress", "pipe:1",
		"-headers", headers,
		"-i", m3u8URL,
		"-c", "copy",
		outputPath,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	var stderrBuf bytes.Buffer
	go func() {
		_, _ = stderrBuf.ReadFrom(stderr)
	}()

	startWall := time.Now()
	var lastPrint time.Time
	var lastSpeed string
	var lastOutTimeMS int64
	var lastTotalSize int64

	printProgress := func(force bool) {
		if !force && time.Since(lastPrint) < 200*time.Millisecond {
			return
		}
		lastPrint = time.Now()

		percentStr := "--"
		if totalDurationSec > 0 && lastOutTimeMS > 0 {
			p := float64(lastOutTimeMS) / (totalDurationSec * 1000.0) * 100.0
			if p < 0 {
				p = 0
			}
			if p > 100 {
				p = 100
			}
			percentStr = fmt.Sprintf("%.2f", p)
		}

		speedStr := strings.TrimSpace(lastSpeed)
		if speedStr == "" {
			speedStr = "--"
		}

		rateStr := ""
		elapsed := time.Since(startWall).Seconds()
		if elapsed > 1 && lastTotalSize > 0 {
			mbps := (float64(lastTotalSize) / 1024.0 / 1024.0) / elapsed
			rateStr = fmt.Sprintf(" %.2fMB/s", mbps)
		}

		fmt.Printf("\r进度: %s%% 速度: %s%s", percentStr, speedStr, rateStr)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "out_time_ms":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				// ffmpeg reports microseconds here despite the name
				lastOutTimeMS = v / 1000
			}
		case "out_time_us":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				lastOutTimeMS = v / 1000
			}
		case "total_size":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				lastTotalSize = v
			}
		case "speed":
			lastSpeed = value
		case "progress":
			printProgress(true)
			if value == "end" {
				fmt.Print("\n")
			}
		}
		printProgress(false)
	}
	_ = scanner.Err()

	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, msg)
	}

	printProgress(true)
	fmt.Print("\n")
	return nil
}

func probeDurationSeconds(m3u8URL string, headers string) float64 {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return probeDurationSecondsFromM3U8(m3u8URL)
	}
	out, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-headers", headers,
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		m3u8URL,
	).Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return probeDurationSecondsFromM3U8(m3u8URL)
	}
	if v <= 0 {
		return probeDurationSecondsFromM3U8(m3u8URL)
	}
	return v
}

func formatDurationSeconds(sec float64) string {
	if sec <= 0 {
		return "未知"
	}
	d := time.Duration(sec * float64(time.Second))
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func probeDurationSecondsFromM3U8(m3u8URL string) float64 {
	const maxDepth = 3
	cur := m3u8URL

	for depth := 0; depth < maxDepth; depth++ {
		text, err := user.MyGetRequest(cur, map[string]string{"Accept": "*/*"})
		if err != nil {
			return 0
		}

		var sum float64
		hasExtinf := false
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#EXTINF:") {
				hasExtinf = true
				v := strings.TrimPrefix(line, "#EXTINF:")
				if idx := strings.IndexByte(v, ','); idx >= 0 {
					v = v[:idx]
				}
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					sum += f
				}
			}
		}
		if hasExtinf && sum > 0 {
			return sum
		}

		next := ""
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if strings.Contains(line, ".m3u8") {
				if resolved, ok := resolveURL(cur, line); ok {
					next = resolved
					break
				}
			}
		}
		if next == "" {
			return 0
		}
		cur = next
	}

	return 0
}

func sanitizeFilePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	reg, _ := regexp.Compile(`[\\/:*?"<>|]`)
	s = reg.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// avoid insanely long filenames
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80])
	}
	return s
}

func (l *Live) tryPopulateLiveMetaFromAPICore() {
	if strings.TrimSpace(l.RoomID) == "" {
		return
	}
	urlStr := config.APIBaseURL() + "/live/v2/live/" + l.RoomID
	resp, err := user.MyGetRequest(urlStr)
	if err != nil {
		return
	}
	if gjson.Get(resp, "code").Int() != 200000 {
		return
	}
	l.title = firstNonEmpty(
		gjson.Get(resp, "data.title").String(),
		gjson.Get(resp, "data.ltitle").String(),
		gjson.Get(resp, "data.liveTitle").String(),
		gjson.Get(resp, "data.name").String(),
	)
	l.date = firstNonEmpty(
		gjson.Get(resp, "data.livedate").String(),
		gjson.Get(resp, "data.liveDate").String(),
		gjson.Get(resp, "data.date").String(),
	)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// recordVOD 根据点播模式的m3u8文件下载快速回放视频
func (l *Live) recordVOD() {
	fmt.Println("开始下载快速回放视频...")
	str, err := user.MyGetRequest(l.quickReplayURL)
	if err != nil {
		fmt.Println("Get请求出错：", err)
		return
	}

	if !strings.Contains(str, "#EXTM3U") {
		if m3u8URL := findFirstM3U8URL(str); m3u8URL != "" {
			l.quickReplayURL = m3u8URL
			str, err = user.MyGetRequest(l.quickReplayURL)
			if err != nil {
				fmt.Println("Get请求出错：", err)
				return
			}
		}
	}

	lines := strings.Split(str, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") { //忽略m3u8文件中的注释行
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, ".m3u8") {
			if resolved, ok := resolveURL(l.quickReplayURL, line); ok {
				l.quickReplayURL = resolved
				l.recordVOD()
				return
			}
		}

		fmt.Println(strings.Split(line, "&")[0], "...")
		if resolved, ok := resolveURL(l.quickReplayURL, line); ok {
			l.newTsURL = resolved
		} else {
			l.newTsURL = l.quickReplayURL[:strings.LastIndex(l.quickReplayURL, "/")+1] + line
		}
		l.downloadAndMergeTsFile()
	}
	fmt.Println("快速回放视频下载完成。")
}
