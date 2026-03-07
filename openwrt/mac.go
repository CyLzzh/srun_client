package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// incrementMAC 接收MAC字符串，将其末位加1并处理可能的字节进位
func incrementMAC(macStr string) (string, error) {
	hwAddr, err := net.ParseMAC(macStr)
	if err != nil {
		return "", err
	}

	// 从后往前逐字节进位加1
	for i := len(hwAddr) - 1; i >= 0; i-- {
		hwAddr[i]++
		if hwAddr[i] != 0 {
			break // 当前字节加1后未溢出（不等于0），停止向高位进位
		}
	}
	return hwAddr.String(), nil
}

// getCurrentWANMAC 优先从 uci 读取 network.wan.macaddr，
// 若未配置则回退到读取 WAN 接口的实际地址。
func getCurrentWANMAC() (string, error) {
	if out, err := exec.Command("uci", "get", "network.wan.macaddr").Output(); err == nil {
		mac := strings.TrimSpace(string(out))
		if mac != "" {
			if _, parseErr := net.ParseMAC(mac); parseErr == nil {
				return mac, nil
			}
		}
	}

	iface := "wan"
	if out, err := exec.Command("uci", "get", "network.wan.device").Output(); err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			iface = v
		}
	} else if out, err := exec.Command("uci", "get", "network.wan.ifname").Output(); err == nil {
		v := strings.TrimSpace(string(out))
		if v != "" {
			iface = v
		}
	}

	addrPath := filepath.Join("/sys/class/net", iface, "address")
	content, err := os.ReadFile(addrPath)
	if err != nil {
		return "", fmt.Errorf("无法获取当前WAN MAC（uci和sysfs都失败）: %w", err)
	}

	mac := strings.TrimSpace(string(content))
	if _, err := net.ParseMAC(mac); err != nil {
		return "", fmt.Errorf("读取到的MAC格式无效: %s", mac)
	}

	return mac, nil
}

func changeWANMACIncrement() (string, error) {
	currentMAC, err := getCurrentWANMAC()
	if err != nil {
		return "", fmt.Errorf("获取当前WAN MAC失败: %w", err)
	}

	newMAC, err := incrementMAC(currentMAC)
	if err != nil {
		return "", fmt.Errorf("MAC解析失败: %w", err)
	}

	// 1. 覆写 network.wan.macaddr 配置项
	uciSet := exec.Command("uci", "set", fmt.Sprintf("network.wan.macaddr=%s", newMAC))
	if err := uciSet.Run(); err != nil {
		return "", fmt.Errorf("uci set 失败: %w", err)
	}

	// 2. 将修改写入闪存
	uciCommit := exec.Command("uci", "commit", "network")
	if err := uciCommit.Run(); err != nil {
		return "", fmt.Errorf("uci commit 失败: %w", err)
	}

	// 3. 重载网络服务使新MAC在接口上生效
	networkReload := exec.Command("/etc/init.d/network", "reload")
	if err := networkReload.Run(); err != nil {
		return "", fmt.Errorf("网络服务重载失败: %w", err)
	}

	return newMAC, nil
}
