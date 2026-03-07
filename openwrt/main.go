package main

import (
	"context"
	"log"
	"net"
	"time"
)

// 检测目标，使用公共 DNS
var targets_ = []string{"223.5.5.5:53", "180.184.1.1:53", "119.29.29.29:53"}

func main() {
	log.Println("🚀 校园网自动登录守护进程已启动 (Router 版)")

	for {
		if isConnected() {
			// 网络正常，进入休眠
			time.Sleep(1 * time.Second) //检测间隔
		} else {
			log.Println("⚠️ 检测到断网，尝试执行登录逻辑...")

			// 先修改 WAN MAC
			if newMAC, err := changeWANMACIncrement(); err != nil {
				log.Printf("⚠️ WAN MAC 修改失败: %v", err)
			} else {
				log.Printf("🔧 WAN MAC 已修改为: %s", newMAC)
			}
			// 给网络一点恢复时间
			time.Sleep(6 * time.Second) //断网后的重试等待时间
			// 调用登录函数
			SrunLogin()
			// 给网络一点恢复时间
			time.Sleep(4 * time.Second) //断网后的重试等待时间
		}
	}
}

// isConnected 检测网络连通性
func isConnected() bool {
	// 1. 快速路径 (Fast Path)：先试探主 DNS
	// 这样做的好处是：网络正常时，只产生 1 个 socket 连接，开销最小
	d := net.Dialer{Timeout: 1 * time.Second} // 快速检测超时设短一点
	conn, err := d.Dial("tcp", targets_[0])
	if err == nil {
		conn.Close()
		return true
	}

	// 2. 慢速路径 (Slow Path)：主 DNS 挂了，并发检测剩余所有节点
	// 这时候才需要动用并发逻辑
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 注意：这里我们检测除了第一个以外的目标
	backupTargets := targets_[1:]
	resultChan := make(chan bool, len(backupTargets))

	for _, t := range backupTargets {
		go func(target string) {
			d := net.Dialer{}
			conn, err := d.DialContext(ctx, "tcp", target)
			if err == nil {
				conn.Close()
				resultChan <- true
				return
			}
			resultChan <- false
		}(t)
	}

	for i := 0; i < len(backupTargets); i++ {
		select {
		case res := <-resultChan:
			if res {
				return true
			}
		case <-ctx.Done():
			return false
		}
	}

	return false
}
