package main

import (
	"context"
	_ "embed"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"
	"github.com/go-toast/toast"
)

//go:embed icon.ico
var iconData []byte

var (
	targets_   = []string{"223.5.5.5:53", "180.184.1.1:53", "114.114.115.115:53", "119.29.29.29:53"}
	cachedIcon string // 缓存临时图标路径，避免重复写入
)

const (
	CheckInterval = 1 * time.Second // 使用 time.Duration，更具可读性
	AppID         = "广海网go深大版v0.1.1"
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
	systray.SetTitle("广海网go深大版v0.1.1")
	systray.SetTooltip("校园网自动重连监控中")
	systray.SetIcon(iconData)

	mQuit := systray.AddMenuItem("退出程序", "退出应用")

	sendNotification("监控已启动", "正在后台守护校园网连接...")

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

			safeLogin() // 封装后的登录函数

			time.Sleep(2 * time.Second) // 登录后稍作等待
		}
	}()

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

// 封装带有 recover 的登录逻辑，减少 main 循环中的冗余代码
func safeLogin() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("❌ 登录逻辑崩溃: %v", r)
		}
	}()
	log.Println("🔄 开始调用 SrunLogin...")
	SrunLogin() // 假设此函数在其他文件中定义
	log.Println("✅ SrunLogin 调用结束")
}

func onExit() {
	log.Println("------ 程序退出 ------")
	if cachedIcon != "" {
		_ = os.Remove(cachedIcon) // 退出时清理临时文件
	}
}

// 优化的网络连接检测：只要有一个通了就立刻返回 true
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
		Icon:    cachedIcon, // 使用预先写好的路径
		Actions: []toast.Action{{Type: "protocol", Label: "忽略", Arguments: ""}},
	}
	if err := notification.Push(); err != nil {
		log.Printf("❌ 通知的发送失败: %v", err)
	}
}
