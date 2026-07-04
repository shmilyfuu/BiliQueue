## BiliQueue v0.1.11

本版本为 v0.1.10 之后的确认发布版。v0.1.11-test1 至 v0.1.11-test6 为本地测试迭代，不单独发布 Release。

### 新增与优化

- Windows 版改为无控制台窗口程序。
- 支持直接双击 `BiliQueue-windows-amd64.exe` 静默启动到系统托盘。
- 将 BiliQueue 图标写入 exe 文件本身，资源管理器中会显示应用图标。
- 增加系统托盘右键菜单：打开控制台、打开 OBS 横条、复制 OBS 地址、清空队列、打开数据文件夹、打开日志文件和退出。
- 增加本地日志文件：`data/logs/biliqueue.log`。
- 将横条文字内容编辑区移动到控制台前部。
- 编辑横条文字时不再实时同步，需点击“应用文字到横条”后才保存并同步到 OBS。
- 将启动脚本改为 `start-BiliQueue.cmd`，避免 Windows / zip / GitHub Actions 间的文件名编码问题。
- Windows 测试包说明文件统一为 `README.md`。

### 修复

- 修复托盘菜单第二次右键可能无法弹出的问题。
- 修复托盘菜单“退出”可能无法结束进程的问题。
- 修复 GUI 模式下日志可能没有输出的问题。
- 降低重复打开控制台或 OBS 横条时页面偶发卡在加载中的概率。
- 统一程序数据目录为 exe 所在目录，直接双击 exe 与 cmd 启动读取同一份 `data`、`fonts`、`assets`。

### 下载

- `BiliQueue-v0.1.11-windows-test.zip`：Windows AMD64 测试包
- `BiliQueue-v0.1.11-source.zip`：完整源码

### 升级提示

需要保留登录状态和现有设置时，请保留旧版的 `data` 文件夹。需要保留自定义字体时，请同时保留 `fonts` 文件夹。`data/auth.json` 含 B 站登录 Cookie，请勿分享。
