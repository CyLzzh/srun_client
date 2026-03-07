# srun_client
自动登录校园网（Srun 认证）的轻量客户端，支持 Windows 和 OpenWrt。

## 适合谁用

- 你使用的是 Srun 校园网认证。
- 你希望设备断网后自动重连，不想每次手动登录。

## 使用前准备

请先编辑配置文件 `account.ini`（Windows 与 OpenWrt 各有一份）：

```ini
[default]
user=你的学号或账号
passwd=你的密码
ACID=你的运营商ID
```

> `ACID` 与学校网络出口/运营商有关，不确定时可先用学校原客户端登录后确认。

---

## Windows 使用

1. 下载并解压 Windows 发布包。
2. 将 `CampusNet.exe` 与 `account.ini` 放在同一目录。
3. 双击运行 `CampusNet.exe`。
4. 程序会在系统托盘常驻，断网后自动尝试重连。

退出方式：右下角托盘图标菜单中点击“退出程序”。

---

## OpenWrt 使用

1. 先确认路由器架构：

```sh
uname -m
```

- `aarch64` 选择 `arm64` 包
- `mipsel` / `mipsle` 选择 `mipsle` 包

2. 上传对应二进制到路由器，例如 `/usr/bin/srunlogin`，并赋可执行权限：

```sh
chmod +x /usr/bin/srunlogin
```

3. 上传并配置 `account.ini`。
4. 运行程序，建议配合 OpenWrt 的启动项（如 `rc.local` 或 procd）实现开机自启。

---

## 发布（维护者）

在仓库根目录执行：

```sh
go run release.go
```

- 脚本会提示输入版本号（也可使用 `go run release.go -version v1.2.3`）。
- 自动构建 Windows / OpenWrt（arm64、mipsle）并打包到 `dist/<版本号>/`。
- 自动创建并推送同名 Git tag（若已存在会跳过创建）。

---

## 常见问题

- **程序能运行但登录失败**：优先检查 `user/passwd/ACID` 是否正确。
- **OpenWrt 无法执行**：通常是架构选错或未 `chmod +x`。
- **频繁重连**：可能是校园网出口波动，可稍后重试。

---

## 鸣谢
[caterpie_szu_srun_client](https://github.com/Caterpie771881/szu_srun_client)
