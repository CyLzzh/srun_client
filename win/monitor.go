package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"github.com/getlantern/systray"
	"github.com/go-toast/toast"
	"golang.org/x/sys/windows/registry"
)

//go:embed icon.ico
var iconData []byte

var (
	targets_   = []string{"223.5.5.5:53", "180.184.1.1:53", "114.114.115.115:53", "119.29.29.29:53"}
	cachedIcon string // 缓存临时图标路径，避免重复写入
)

const (
	CheckInterval  = 1 * time.Second
	CurrentVersion = "0.1.3"
	AppID          = "广海网v" + CurrentVersion
	RunKeyPath     = `Software\Microsoft\Windows\CurrentVersion\Run`
	RunValueName   = "CampusNetMonitor"
	VersionURL     = "https://dagongren.tech:44095/srunversion.txt"
	StatusPageURL  = "https://dagongren.tech:44095/info.htm"
	UpdatePageURL  = "https://blog.csdn.net/2304_80029632/article/details/158468587"
)

func main() {
	// 日志配置
	setupLogging()

	// 预先准备通知图标，避免每次通知都写磁盘
	prepareIcon()

	// 启动系统托盘
	systray.Run(onReady, onExit)
}

func setupLogging() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	workDir := filepath.Dir(exePath)
	logPath := filepath.Join(workDir, "log.txt")

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(file)
		log.Println("------ 程序启动 ------")
	}
}

// 将嵌入的图标写入临时文件一次
func prepareIcon() {
	tempDir := os.TempDir()
	cachedIcon = filepath.Join(tempDir, "campus_net_temp_icon.ico")
	_ = os.WriteFile(cachedIcon, iconData, 0644)
}

func onReady() {
	systray.SetTitle("广海网v" + CurrentVersion)
	systray.SetTooltip("校园网自动重连监控中")
	systray.SetIcon(iconData)

	autoStartEnabled, err := isAutoStartEnabled()
	if err != nil {
		log.Printf("⚠️ 读取开机自启动状态失败: %v", err)
		autoStartEnabled = false
	}
	mAutoStart := systray.AddMenuItemCheckbox("开机自启动", "切换开机自动运行", autoStartEnabled)
	mCheckUpdate := systray.AddMenuItem("检查更新", "检查是否有新版本")
	mViewStatus := systray.AddMenuItem("查看校园网状态", "打开校园网状态页面")

	mQuit := systray.AddMenuItem("退出程序", "退出应用")

	sendNotification("监控已启动", "正在后台守护校园网连接...")
	go checkForUpdates(false)

	// 监控协程
	go func() {
		log.Println("🚀 监控协程启动...")
		for {
			if isConnected() {
				time.Sleep(CheckInterval)
				continue
			}

			log.Println("⚠️ 检测到断网，尝试重连...")
			sendNotification("网络断开", "正在自动重连校园网...")
			safeLogin()                 // 封装后的登录函数
			time.Sleep(2 * time.Second) // 登录后稍作等待
		}
	}()

	go func() {
		for range mAutoStart.ClickedCh {
			enable := !mAutoStart.Checked()
			if err := setAutoStartEnabled(enable); err != nil {
				log.Printf("❌ 设置开机自启动失败: %v", err)
				sendNotification("开机自启动设置失败", "请检查程序权限后重试")
				continue
			}

			if enable {
				mAutoStart.Check()
				log.Println("✅ 已开启开机自启动")
				sendNotification("开机自启动已开启", "下次开机将自动运行监控")
			} else {
				mAutoStart.Uncheck()
				log.Println("ℹ️ 已关闭开机自启动")
				sendNotification("开机自启动已关闭", "下次开机将不再自动运行")
			}
		}
	}()

	go func() {
		for range mCheckUpdate.ClickedCh {
			checkForUpdates(true)
		}
	}()

	go func() {
		for range mViewStatus.ClickedCh {
			if err := openURL(StatusPageURL); err != nil {
				log.Printf("❌ 打开校园网状态页面失败: %v", err)
				sendNotification("打开页面失败", "请稍后重试")
			}
		}
	}()

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

func isAutoStartEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, RunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}
	defer key.Close()

	value, _, err := key.GetStringValue(RunValueName)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, nil
		}
		return false, err
	}

	exePath, err := os.Executable()
	if err != nil {
		return false, err
	}

	expectedValue := `"` + exePath + `"`
	return strings.EqualFold(value, expectedValue), nil
}

func setAutoStartEnabled(enable bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, RunKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enable {
		err = key.DeleteValue(RunValueName)
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	return key.SetStringValue(RunValueName, `"`+exePath+`"`)
}

// 封装带有 recover 的登录逻辑，减少 main 循环中的冗余代码
func safeLogin() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("❌ 登录逻辑崩溃: %v", r)
		}
	}()
	log.Println("🔄 开始调用 SrunLogin...")
	SrunLogin()
	log.Println("✅ SrunLogin 调用结束")
}

func onExit() {
	log.Println("------ 程序退出 ------")
	if cachedIcon != "" {
		_ = os.Remove(cachedIcon) // 退出时清理临时文件
	}
}

// 网络连接检测：只要有一个通了就立刻返回 true
func isConnected() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 使用 WaitGroup 或 context 协助，但最简单的是只要有一个成功就返回
	// 这里采用并发探测，一旦成功立即返回的模式
	connected := make(chan bool, len(targets_))

	for _, t := range targets_ {
		go func(target string) {
			d := net.Dialer{}
			conn, err := d.DialContext(ctx, "tcp", target)
			if err == nil {
				conn.Close()
				select {
				case connected <- true:
				default:
				}
				return
			}
			connected <- false
		}(t)
	}

	// 收集结果
	for range targets_ {
		if <-connected {
			return true
		}
	}
	return false
}

func sendNotification(title, message string) {
	notification := toast.Notification{
		AppID:   AppID,
		Title:   title,
		Message: message,
		Icon:    cachedIcon,
		Actions: []toast.Action{{Type: "protocol", Label: "忽略", Arguments: ""}},
	}
	if err := notification.Push(); err != nil {
		log.Printf("❌ 通知发送失败: %v", err)
	}
}

func checkForUpdates(manual bool) {
	latestVersion, err := fetchLatestVersion()
	if err != nil {
		log.Printf("⚠️ 检查更新失败: %v", err)
		if isConnected() {
			sendNotification("检查更新失败", "作者服务器过期了")
		} else {
			sendNotification("检查更新失败", "无网络")
		}
		return
	}

	cmp := compareVersion(latestVersion, CurrentVersion)
	if cmp > 0 {
		sendNotification("发现新版本", fmt.Sprintf("当前 %s，最新 %s，正在打开更新页面", CurrentVersion, latestVersion))
		if err := openURL(UpdatePageURL); err != nil {
			log.Printf("❌ 打开更新页面失败: %v", err)
			sendNotification("更新页面打开失败", "请手动访问更新地址")
		}
		return
	}

	if manual {
		sendNotification("检查更新", fmt.Sprintf("当前已是最新版本：%s", CurrentVersion))
	}
}

func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(VersionURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("版本接口返回状态码: %d", resp.StatusCode)
	}

	buf := make([]byte, 32)
	n, err := resp.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return "", err
	}

	version := strings.TrimSpace(string(buf[:n]))
	if version == "" {
		return "", fmt.Errorf("版本号为空")
	}

	return version, nil
}

func compareVersion(a, b string) int {
	ap := strings.Split(strings.TrimSpace(a), ".")
	bp := strings.Split(strings.TrimSpace(b), ".")
	maxLen := len(ap)
	if len(bp) > maxLen {
		maxLen = len(bp)
	}

	for i := 0; i < maxLen; i++ {
		av := 0
		bv := 0
		if i < len(ap) {
			if n, err := strconv.Atoi(ap[i]); err == nil {
				av = n
			}
		}
		if i < len(bp) {
			if n, err := strconv.Atoi(bp[i]); err == nil {
				bv = n
			}
		}

		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}

	return 0
}

func openURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
