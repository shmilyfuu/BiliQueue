# BiliQueue

BiliQueue 是一个本地运行的 B 站直播横向排队工具。程序提供网页控制台和 OBS 浏览器横条，可通过弹幕管理排队、取消排队和礼物插队。

> 当前正式版本：v0.1.11
>
> 当前仍处于测试阶段。B 站直播接口可能发生调整，建议优先使用 Release 中的最新版本。

## 功能

- 使用直播间号连接 B 站直播弹幕
- B 站扫码登录，登录信息仅保存在本机项目目录
- 通过弹幕指令加入或取消排队
- 当前用户、等待队列和说明区域独立显示
- 三个区域宽度及横条统一高度可调整
- 单行、双行队列及横向滚动
- 普通弹幕用户头像获取与本机缓存
- 用户头像大小可调整
- 支持程序目录 `fonts` 文件夹中的自定义字体
- 按电池门槛触发礼物插队
- 礼物优先区可选择按单次礼物价值排序
- 手动新增、删除、拖动排序、上移和下移
- 当前用户也可参与队列排序
- 全局背景透明度渐变和三区独立背景
- 横条文字内容支持手动应用，编辑时不会实时同步到 OBS
- Windows 版支持直接双击 exe 静默启动到系统托盘
- 托盘菜单支持打开控制台、打开 OBS 横条、复制 OBS 地址、清空队列、打开数据文件夹、打开日志和退出
- 当天队列异常退出恢复
- 配置导入、导出与导入前自动备份

## 下载与运行

请从仓库的 **Releases** 页面下载：

```text
BiliQueue-v0.1.11-windows-test.zip
```

解压后推荐直接双击：

```text
BiliQueue-windows-amd64.exe
```

正常情况下不会出现黑色控制台窗口，也不会自动打开浏览器。启动后请在 Windows 右下角系统托盘区域找到 BiliQueue 图标，右键打开菜单。

备用启动脚本：

```text
start-BiliQueue.cmd
```

## 地址

控制台地址：

```text
http://127.0.0.1:18303/control
```

OBS 浏览器来源地址：

```text
http://127.0.0.1:18303/overlay
```

OBS 浏览器来源的宽度和高度，请按照控制台顶部预览区域显示的建议值填写。

## 基本使用

1. 启动 BiliQueue。
2. 首次使用时，在控制台点击“扫码登录”。
3. 填写直播间号并连接。
4. 观众发送 `排队` 加入队列，发送 `取消排队` 退出队列。
5. 在 OBS 中添加浏览器来源并填写横条地址。
6. 根据直播需求调整区域尺寸、文字样式、滚动方式、背景渐变和礼物规则。

弹幕指令、空态文字、右侧说明内容和文字样式均可在控制台修改。

## v0.1.11 重点变化

- Windows 版改为无控制台窗口程序。
- 推荐直接双击 `BiliQueue-windows-amd64.exe` 静默启动到系统托盘。
- 将 BiliQueue 图标写入 exe 文件本身，资源管理器中会显示应用图标。
- 托盘右键菜单支持打开控制台、打开 OBS 横条、复制 OBS 地址、清空队列、打开数据文件夹、打开日志文件和退出。
- 增加本地日志文件：`data/logs/biliqueue.log`。
- 将横条文字内容编辑区移动到控制台前部。
- 编辑横条文字时不再实时同步，需点击“应用文字到横条”后才保存并同步到 OBS。
- 将启动脚本改为 `start-BiliQueue.cmd`，避免 Windows / zip / GitHub Actions 间的文件名编码问题。
- Windows 测试包说明文件统一为 `README.md`。

## 字体文件夹

程序目录下提供：

```text
fonts/
```

将字体文件复制到 `fonts` 后，在控制台点击“刷新字体列表”。程序不会附带字体文件，字体文件由使用者自行准备，并需确认授权范围。

## 礼物插队

礼物插队按单次付费礼物价值判断，低于门槛的多次礼物不会累计。

关闭“按礼物价值排序”时，新触发插队的用户会追加到现有礼物优先区末尾：

```text
当前A → D（先送礼）→ C（后送礼）→ 普通队列
```

开启后，礼物优先区按最近一次达到门槛的单次礼物价值从高到低排列。

免费礼物默认不参与电池门槛计算。

## 数据位置与升级

所有运行数据均保存在程序所在目录的 `data` 文件夹中，自定义字体保存在 `fonts` 文件夹中：

```text
data/
├── auth.json
├── config.json
├── queue-today.json
├── logs/
├── avatars/
└── backups/

fonts/
└── .gitkeep
```

其中 `auth.json` 含 B 站登录 Cookie，请勿分享或提交到 GitHub。

升级方式：

- 在原目录覆盖新版 EXE，可以继续使用原登录状态、配置和字体。
- 解压到新目录时，将旧版 `data` 和 `fonts` 文件夹复制到新目录。

仓库已通过 `.gitignore` 排除用户数据、缓存、日志、构建产物、压缩包和登录信息。

## 配置导入与导出

控制台支持导出和导入配置。

导出的配置包括直播间、指令、礼物规则、OBS 布局、文字样式和背景样式，不包括：

- B 站登录 Cookie
- 当前队列
- 当天队列快照
- 头像缓存
- 字体文件
- 最近弹幕或礼物记录

导入前，当前配置会自动备份到 `data/backups`。

## 源码构建

要求：

- Go 1.23 或更高版本
- 无外部 Go 模块依赖
- Windows 图标嵌入由 `tools/embed_icon_pe.py` 处理

运行测试：

```bash
go test ./...
go test -race ./...
go vet ./...
```

Windows AMD64 构建：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-H=windowsgui -s -w" -o BiliQueue-windows-amd64.exe .
python tools/embed_icon_pe.py BiliQueue-windows-amd64.exe assets/biliqueue.ico BiliQueue-windows-amd64-icon.exe
mv BiliQueue-windows-amd64-icon.exe BiliQueue-windows-amd64.exe
```

## 版本发布规则

仓库只提交已经确认的正式版本。后续测试包按以下方式命名：

```text
v0.1.12-test1  本地测试包，不提交仓库
v0.1.12-test2  本地测试包，不提交仓库
v0.1.12        确认后提交仓库并创建 Release
```

未确认的测试版本不会直接写入仓库。

## 隐私说明

BiliQueue 为本地程序。账号 Cookie、配置、队列、日志和头像缓存保存在本机项目目录。使用源码或发布包时，请自行保护 `data` 文件夹中的敏感内容。
