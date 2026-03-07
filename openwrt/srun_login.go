package main

import (
	// "bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"srunClient/encryptlib"
	"time"
	"gopkg.in/ini.v1"
)

var targets = map[string]string{
	"rad_user_info": "http://10.129.1.1/cgi-bin/rad_user_info",
	"get_challenge": "http://10.129.1.1/cgi-bin/get_challenge",
	"srun_portal":   "http://10.129.1.1/cgi-bin/srun_portal",
}

const (
	callback  string = "jQueryCallback"
	userAgent string = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
	TYPE      string = "1"
	N         string = "200"
	ENC       string = "srun_bx1"
)

// 全局复用 Client，启用长连接
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

type getIpResp struct {
	Ip string `json:"online_ip"`
}

type getChallengeResp struct {
	Challenge string `json:"challenge"`
}

// 辅助函数：通用请求处理
// 负责发送请求、读取 Body、处理 JSONP 包装
func doRequest(uri string, params url.Values, target interface{}) error {
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return err
	}

	req.URL.RawQuery = params.Encode()
	req.Header.Add("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 安全处理 JSONP：去除 "jQueryCallback(...)" 和最后的 ")"
	// 检查长度防止 panic
	prefixLen := len(callback) + 1
	if len(body) > prefixLen+1 {
		// 这里假设服务器返回严格符合 callback(...) 格式
		// 如果可能返回纯 JSON，需要加判断
		body = body[prefixLen : len(body)-1]
	}

	// 如果 target 为 nil，说明不需要解析返回值（例如 login 接口可能只需要发包）
	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			// 如果解析失败，可能是服务器返回了非 JSON 的错误信息，打印出来方便调试
			return fmt.Errorf("解析 JSON 失败: %v, Body: %s", err, string(body))
		}
	}
	return nil
}

// 获取 IP 地址
func getIp(callback, path string) (string, error) {
	params := make(url.Values)
	params.Set("callback", callback)

	var resp getIpResp
	if err := doRequest(path, params, &resp); err != nil {
		return "", err
	}
	return resp.Ip, nil
}

// 获取 Challenge Token
func getChallenge(callback, username, ip, path string) (string, error) {
	params := make(url.Values)
	params.Set("callback", callback)
	params.Set("username", username)
	params.Set("ip", ip)

	var resp getChallengeResp
	if err := doRequest(path, params, &resp); err != nil {
		return "", err
	}
	return resp.Challenge, nil
}

// 核心登录逻辑
func srunPortalLogin(username, password, acid, token, ip string) {
	hmd5_password := encryptlib.Hmd5(password, token)
	info := encryptlib.GetInfo(encryptlib.Info{
		Username: username,
		Password: password,
		Ip:       ip,
		Acid:     acid,
		EncVer:   ENC,
	}, token)

	chksum := encryptlib.Sha1(
		encryptlib.Chkstr(token, username, hmd5_password, acid, ip, N, TYPE, info))

	currentTime := fmt.Sprintf("%d000", time.Now().Unix())

	params := make(url.Values)
	params.Set("action", "login")
	params.Set("callback", callback)
	params.Set("username", username)
	params.Set("password", "{MD5}"+hmd5_password)
	params.Set("os", "Kindle")
	params.Set("name", "Kindle")
	params.Set("double_stack", "0")
	params.Set("chksum", chksum)
	params.Set("info", info)
	params.Set("ac_id", acid)
	params.Set("ip", ip)
	params.Set("n", N)
	params.Set("type", TYPE)
	params.Set("_", currentTime)

	// 这里的响应结构体如果你不需要处理，可以传 nil，或者定义一个通用的 map
	// 登录接口通常只看 HTTP 状态码或 body 里的 error 字段，这里为了简化暂不解析详细内容
	err := doRequest(targets["srun_portal"], params, nil)
	if err != nil {
		log.Printf("[!] 登录请求发送失败: %v\n", err)
	} else {
		log.Println("[+] 登录请求发送成功")
	}
}

func SrunLogin() {
	exePath, err := os.Executable()
	if err != nil {
		log.Println("[!] 获取程序路径失败")
		return
	}
	accountPath := filepath.Join(filepath.Dir(exePath), "account.ini")
	cfg, err := ini.Load(accountPath)
	if err != nil {
		log.Printf("[!] 无法读取 account.ini 文件: %v\n", err)
		return
	}

	section := cfg.Section("default")
	user := section.Key("user").String()
	passwd := section.Key("passwd").String()
	acid := section.Key("ACID").String()

	if user == "" || passwd == "" {
		log.Println("[!] 配置文件中 user 或 passwd 为空")
		return
	}

	// 2. 自动获取 IP
	ip, err := getIp(callback, targets["rad_user_info"])
	if err != nil {
		log.Printf("[!] 获取 IP 失败: %v\n", err)
		return
	}
	log.Printf("[*] 获取 IP 成功: %s\n", ip)

	// 3. 获取 Token
	token, err := getChallenge(callback, user, ip, targets["get_challenge"])
	if err != nil {
		log.Printf("[!] 获取 Token 失败: %v\n", err)
		return
	}
	log.Printf("[*] 获取 Token 成功: %s\n", token)

	// 4. 执行登录
	srunPortalLogin(user, passwd, acid, token, ip)
}