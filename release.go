package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type buildTarget struct {
	workDir string
	env     map[string]string
	output  string
	ldflags string
}

func main() {
	versionFlag := flag.String("version", "", "版本号，例如 v1.2.3")
	flag.Parse()

	version, err := resolveVersion(*versionFlag)
	if err != nil {
		fatal(err)
	}

	root, err := os.Getwd()
	if err != nil {
		fatal(fmt.Errorf("获取当前目录失败: %w", err))
	}

	distRoot := filepath.Join(root, "dist")
	distDir := filepath.Join(distRoot, version)
	if err := os.RemoveAll(distDir); err != nil {
		fatal(fmt.Errorf("清理旧产物失败: %w", err))
	}
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		fatal(fmt.Errorf("创建产物目录失败: %w", err))
	}

	winBuildDir := filepath.Join(root, "win")
	openwrtBuildDir := filepath.Join(root, "openwrt")

	winExe := filepath.Join(distDir, fmt.Sprintf("CampusNet-windows-amd64-%s.exe", version))
	arm64Bin := filepath.Join(distDir, fmt.Sprintf("srunlogin-openwrt-arm64-%s", version))
	mipsleBin := filepath.Join(distDir, fmt.Sprintf("srunlogin-openwrt-mipsle-%s", version))

	fmt.Println("[1/6] 构建 Windows 版本...")
	if err := invokeGoBuild(buildTarget{
		workDir: winBuildDir,
		env: map[string]string{
			"GOOS":   "windows",
			"GOARCH": "amd64",
		},
		output:  winExe,
		ldflags: "-s -w -H windowsgui",
	}); err != nil {
		fatal(err)
	}

	fmt.Println("[2/6] 构建 OpenWrt arm64 版本...")
	if err := invokeGoBuild(buildTarget{
		workDir: openwrtBuildDir,
		env: map[string]string{
			"GOOS":   "linux",
			"GOARCH": "arm64",
		},
		output:  arm64Bin,
		ldflags: "-s -w",
	}); err != nil {
		fatal(err)
	}

	fmt.Println("[3/6] 构建 OpenWrt mipsle 版本...")
	if err := invokeGoBuild(buildTarget{
		workDir: openwrtBuildDir,
		env: map[string]string{
			"GOOS":   "linux",
			"GOARCH": "mipsle",
			"GOMIPS": "softfloat",
		},
		output:  mipsleBin,
		ldflags: "-s -w",
	}); err != nil {
		fatal(err)
	}

	fmt.Println("[4/6] 创建并推送 Git tag...")
	if err := ensureAndPushTag(root, version); err != nil {
		fatal(err)
	}

	fmt.Println("[5/6] 上传可执行文件至 GitHub Releases...")
	if err := uploadToGitHubRelease("CyLzzh/srun_client", version, []string{winExe, arm64Bin, mipsleBin}); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 上传 GitHub Releases 失败: %v\n", err)
	}

	fmt.Println("[6/6] 输出产物列表...")
	fmt.Println()
	fmt.Printf("Release 打包完成，产物目录: %s\n", distDir)
	fmt.Printf("Git tag 已推送: %s\n", version)
	if err := listArtifacts(distDir); err != nil {
		fatal(err)
	}
}

// ---------------- 以下为新增的 GitHub 交互逻辑 ----------------

type githubRelease struct {
	ID        int    `json:"id"`
	UploadURL string `json:"upload_url"`
}

type githubAsset struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func uploadToGitHubRelease(repo, version string, files []string) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return errors.New("缺少 GITHUB_TOKEN 环境变量，跳过上传")
	}

	client := &http.Client{}
	release, err := getOrCreateRelease(client, token, repo, version)
	if err != nil {
		return err
	}

	uploadBaseURL := strings.TrimSpace(strings.Split(release.UploadURL, "{")[0])
	if uploadBaseURL == "" {
		uploadBaseURL = fmt.Sprintf("https://uploads.github.com/repos/%s/releases/%d/assets", repo, release.ID)
	}

	for _, file := range files {
		fmt.Printf("  -> 正在上传 %s...\n", filepath.Base(file))
		if err := deleteAssetIfExists(client, token, repo, release.ID, filepath.Base(file)); err != nil {
			return fmt.Errorf("删除旧资产失败 (%s): %w", filepath.Base(file), err)
		}
		if err := uploadAsset(client, token, uploadBaseURL, file); err != nil {
			return fmt.Errorf("上传 %s 失败: %w", filepath.Base(file), err)
		}
	}

	return nil
}

func getOrCreateRelease(client *http.Client, token, repo, version string) (githubRelease, error) {
	var release githubRelease

	getURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, url.PathEscape(version))
	req, err := http.NewRequest("GET", getURL, nil)
	if err != nil {
		return release, err
	}
	addGitHubHeaders(req, token)

	resp, err := client.Do(req)
	if err != nil {
		return release, fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return release, fmt.Errorf("解析 Release 信息失败: %w", err)
		}
		return release, nil
	case http.StatusNotFound:
		createURL := fmt.Sprintf("https://api.github.com/repos/%s/releases", repo)
		payload := map[string]any{
			"tag_name": version,
			"name":     version,
		}
		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return release, err
		}

		reqCreate, err := http.NewRequest("POST", createURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return release, err
		}
		addGitHubHeaders(reqCreate, token)
		reqCreate.Header.Set("Content-Type", "application/json")

		respCreate, err := client.Do(reqCreate)
		if err != nil {
			return release, fmt.Errorf("创建 Release 请求失败: %w", err)
		}
		defer respCreate.Body.Close()

		if respCreate.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(respCreate.Body)
			return release, fmt.Errorf("创建 Release 失败，状态码: %d, 响应: %s", respCreate.StatusCode, strings.TrimSpace(string(body)))
		}
		if err := json.NewDecoder(respCreate.Body).Decode(&release); err != nil {
			return release, fmt.Errorf("解析创建后的 Release 信息失败: %w", err)
		}
		return release, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return release, fmt.Errorf("获取 Release 失败，状态码: %d, 响应: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

func deleteAssetIfExists(client *http.Client, token, repo string, releaseID int, assetName string) error {
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/%d/assets", repo, releaseID)
	req, err := http.NewRequest("GET", listURL, nil)
	if err != nil {
		return err
	}
	addGitHubHeaders(req, token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("查询资产失败，状态码: %d, 响应: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var assets []githubAsset
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return fmt.Errorf("解析资产列表失败: %w", err)
	}

	for _, asset := range assets {
		if asset.Name != assetName {
			continue
		}

		deleteURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/assets/%d", repo, asset.ID)
		reqDelete, err := http.NewRequest("DELETE", deleteURL, nil)
		if err != nil {
			return err
		}
		addGitHubHeaders(reqDelete, token)

		respDelete, err := client.Do(reqDelete)
		if err != nil {
			return err
		}
		defer respDelete.Body.Close()

		if respDelete.StatusCode != http.StatusNoContent {
			body, _ := io.ReadAll(respDelete.Body)
			return fmt.Errorf("删除旧资产失败，状态码: %d, 响应: %s", respDelete.StatusCode, strings.TrimSpace(string(body)))
		}

		fmt.Printf("  -> 已删除同名旧文件 %s，准备重新上传\n", assetName)
		break
	}

	return nil
}

func addGitHubHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

func uploadAsset(client *http.Client, token, baseURL, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	uploadURL := fmt.Sprintf("%s?name=%s", baseURL, url.QueryEscape(filepath.Base(filePath)))
	req, err := http.NewRequest("POST", uploadURL, file)
	if err != nil {
		return err
	}

	addGitHubHeaders(req, token)
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = stat.Size()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API 响应错误 (状态码: %d): %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// ---------------- 以下为原有的基础函数，保持不变 ----------------

func resolveVersion(v string) (string, error) {
	version := strings.TrimSpace(v)
	if version != "" {
		return version, nil
	}

	fmt.Print("请输入版本号 (例如 v1.2.3): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("读取版本号失败: %w", err)
	}

	version = strings.TrimSpace(input)
	if version == "" {
		return "", errors.New("版本号不能为空")
	}
	return version, nil
}

func invokeGoBuild(target buildTarget) error {
	if err := runCmd(target.workDir, nil, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy 失败 (%s): %w", target.workDir, err)
	}

	buildArgs := []string{"build", "-ldflags", target.ldflags, "-o", target.output, "."}
	if err := runCmd(target.workDir, target.env, "go", buildArgs...); err != nil {
		return fmt.Errorf("go build 失败 (%s): %w", target.workDir, err)
	}
	return nil
}

func ensureAndPushTag(root, version string) error {
	tagOut, err := runCmdCapture(root, nil, "git", "tag", "--list", version)
	if err != nil {
		return fmt.Errorf("查询 Git tag 失败，请确认已安装 Git 且当前目录是仓库: %w", err)
	}

	if strings.TrimSpace(tagOut) == "" {
		if err := runCmd(root, nil, "git", "tag", version); err != nil {
			return fmt.Errorf("创建 Git tag 失败: %w", err)
		}
	} else {
		fmt.Printf("Tag %s 已存在，跳过创建。\n", version)
	}

	if err := runCmd(root, nil, "git", "push", "origin", version); err != nil {
		return fmt.Errorf("推送 Git tag 失败，请检查远程仓库和权限: %w", err)
	}
	return nil
}

func runCmd(workDir string, extraEnv map[string]string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = mergedEnv(extraEnv)
	return cmd.Run()
}

func runCmdCapture(workDir string, extraEnv map[string]string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = workDir
	cmd.Env = mergedEnv(extraEnv)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

func mergedEnv(extra map[string]string) []string {
	envMap := map[string]string{}
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	for key, val := range extra {
		envMap[key] = val
	}
	if runtime.GOOS == "windows" {
		envMap["CGO_ENABLED"] = "0"
	}

	merged := make([]string, 0, len(envMap))
	for key, val := range envMap {
		merged = append(merged, key+"="+val)
	}
	return merged
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开文件失败 (%s): %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目录失败 (%s): %w", filepath.Dir(dst), err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建文件失败 (%s): %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("复制文件失败 (%s -> %s): %w", src, dst, err)
	}
	return nil
}

func createZipFromDir(srcDir, dstZip string) error {
	file, err := os.Create(dstZip)
	if err != nil {
		return fmt.Errorf("创建 ZIP 失败 (%s): %w", dstZip, err)
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		w, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, src); err != nil {
			return err
		}
		return nil
	})
}

func createTarGzFromDir(srcDir, dstTarGz string) error {
	file, err := os.Create(dstTarGz)
	if err != nil {
		return fmt.Errorf("创建 tar.gz 失败 (%s): %w", dstTarGz, err)
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		info, err := d.Info()
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		if _, err := io.Copy(tarWriter, src); err != nil {
			return err
		}
		return nil
	})
}

func listArtifacts(distDir string) error {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		return fmt.Errorf("读取产物目录失败: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Printf("- %s (%d bytes)\n", entry.Name(), info.Size())
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	os.Exit(1)
}
