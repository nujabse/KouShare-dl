package live

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
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

	outputName := fmt.Sprintf("replay_room%s_vid%s_%sp_%s.mp4",
		l.RoomID,
		l.VideoID,
		strconv.FormatInt(bestHeight, 10),
		time.Now().Format("2006-01-02_15-04-05"),
	)
	outputPath := filepath.Join(l.SaveDir, outputName)

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

	cmd := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-y",
		"-headers", headers,
		"-i", m3u8URL,
		"-c", "copy",
		outputPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
