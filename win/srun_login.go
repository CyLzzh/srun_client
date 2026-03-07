package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"srunClient/encryptlib" // 假设这是你的本地包

	"gopkg.in/ini.v1"
)

// 使用 const 替代全局 map，减少运行时内存分配，编译器可优化
const (
	Host        = "10.129.1.1"
	BaseURL     = "http://" + Host + "/cgi-bin"
	URLUserInfo = BaseURL + "/rad_user_info"
	URLChallenge= BaseURL + "/get_challenge"
	URLPortal   = BaseURL + "/srun_portal"

	Callback  = "jQueryCallback"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36"
	Type      = "1"
	N         = "200"
	Enc       = "srun_bx1"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

type getIpResp struct {
	Ip string `json:"online_ip"`
}

type getChallengeResp struct {
	Challenge string `json:"challenge"`
}

// doRequest 发送请求并处理结果
// 优化：target 为 nil 时不进行 JSON 解析，但返回 body 内容以供调试（如果需要）
func doRequest(uri string, params url.Values, target interface{}) error {
	// 拼接 URL 能够避免 NewRequest 的部分解析开销，且更直观
	fullURL := fmt.Sprintf("%s?%s", uri, params.Encode())

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 如果不需要解析（例如登录接口我们只关心是否报错，或者手动打印 Body），直接返回
	if target == nil {
		// 这里可以选择是否读取 Body 打印日志，如果完全不关心，可以直接 return nil
		// 但为了调试方便，通常建议看一下响应
		// io.Copy(io.Discard, resp.Body) 
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// 优化：更安全的 JSONP 去除方式，防止 panic
	// 查找第一个 '(' 和 最后一个 ')'
	start := bytes.IndexByte(body, '(')
	end := bytes.LastIndexByte(body, ')')

	if start != -1 && end != -1 && end > start {
		body = body[start+1 : end]
	}

	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("JSON 解析失败: %v | Raw: %s", err, string(body))
	}

	return nil
}

func getIp() (string, error) {
	params := url.Values{}
	params.Set("callback", Callback)

	var resp getIpResp
	if err := doRequest(URLUserInfo, params, &resp); err != nil {
		return "", err
	}
	return resp.Ip, nil
}

// 移除冗余参数 callback 和 path，因为它们是常量
func getChallenge(username, ip string) (string, error) {
	params := url.Values{}
	params.Set("callback", Callback)
	params.Set("username", username)
	params.Set("ip", ip)

	var resp getChallengeResp
	if err := doRequest(URLChallenge, params, &resp); err != nil {
		return "", err
	}
	return resp.Challenge, nil
}

func srunPortalLogin(username, password, acid, token, ip string) {
	hmd5Password := encryptlib.Hmd5(password, token)
	
	infoObj := encryptlib.Info{
		Username: username,
		Password: password,
		Ip:       ip,
		Acid:     acid,
		EncVer:   Enc,
	}
	info := encryptlib.GetInfo(infoObj, token)

	chkStr := encryptlib.Chkstr(token, username, hmd5Password, acid, ip, N, Type, info)
	chksum := encryptlib.Sha1(chkStr)

	// 使用 UnixMilli 更精准（Go 1.17+），或者保持你的写法
	// currentTime := fmt.Sprintf("%d", time.Now().UnixMilli()) 
	currentTime := fmt.Sprintf("%d000", time.Now().Unix())

	v := url.Values{}
	v.Set("action", "login")
	v.Set("callback", Callback)
	v.Set("username", username)
	v.Set("password", "{MD5}"+hmd5Password)
	v.Set("os", "Windows 10") // 修改为常见 OS，防止被某些审计规则拦截
	v.Set("name", "Windows")
	v.Set("double_stack", "0")
	v.Set("chksum", chksum)
	v.Set("info", info)
	v.Set("ac_id", acid)
	v.Set("ip", ip)
	v.Set("n", N)
	v.Set("type", Type)
	v.Set("_", currentTime)

	if err := doRequest(URLPortal, v, nil); err != nil {
		log.Printf("[!] 登录请求发送失败: %v", err)
	} else {
		log.Println("[+] 登录请求已发送 (请检查网络连接以确认成功)")
	}
}

func SrunLogin() {
	// --- 路径处理逻辑 (保留并增强) ---
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("[!] 致命错误：无法获取程序路径: %v", err)
	}
	
	// 处理符号链接的情况 (更稳健)
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}
	
	baseDir := filepath.Dir(realPath)
	accountPath := filepath.Join(baseDir, "account.ini")

	log.Printf("[*] 正在加载配置文件: %s", accountPath)
	// --------------------------------

	cfg, err := ini.Load(accountPath)
	if err != nil {
		log.Printf("[!] 无法读取 account.ini: %v\n请确保文件存在于程序同一目录下", err)
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

	ip, err := getIp()
	if err != nil {
		log.Printf("[!] 获取 IP 失败: %v", err)
		return
	}
	log.Printf("[*] 本机 IP: %s", ip)

	token, err := getChallenge(user, ip)
	if err != nil {
		log.Printf("[!] 获取 Token 失败: %v", err)
		return
	}
	log.Printf("[*] Token: %s", token)

	srunPortalLogin(user, passwd, acid, token, ip)
}