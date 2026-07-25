package nativeui

type targetDefinition struct {
	Title       string
	Message     string
	ConfirmText string
	CancelText  string
}

var targetDefinitions = map[string]targetDefinition{
	"portChange": {
		Title:       "修改监听地址",
		Message:     "修改后程序会重新启动本地服务。",
		ConfirmText: "确定",
		CancelText:  "取消",
	},
	"portConflict": {
		Title:       "啊哦！",
		Message:     "当前端口已被占用，请输入一个新的监听端口。",
		ConfirmText: "重新启动",
		CancelText:  "取消",
	},
	"clearQueue": {
		Title:       "清空队列",
		Message:     "确定清空当前队列吗？此操作无法撤销。",
		ConfirmText: "清空队列",
		CancelText:  "取消",
	},
	"genericConfirm": {
		Title:       "请确认",
		Message:     "这是项目中通用的双按钮确认窗口。",
		ConfirmText: "确定",
		CancelText:  "取消",
	},
	"genericInfo": {
		Title:       "BiliQueue",
		Message:     "这是项目中通用的单按钮信息提示窗口。",
		ConfirmText: "确定",
	},
	"duplicateInstance": {
		Title:       "啊哦！",
		Message:     "BiliQueue 已经在运行，请在系统托盘中找到程序图标。",
		ConfirmText: "知道了",
	},
	"genericError": {
		Title:       "操作失败",
		Message:     "操作没有完成。详细错误信息会显示在这里。",
		ConfirmText: "确定",
	},
	"startupFailed": {
		Title:       "启动失败",
		Message:     "BiliQueue 启动失败，错误详情会显示在这里。",
		ConfirmText: "确定",
	},
	"copyURLFailed": {
		Title:       "复制失败",
		Message:     "浏览器源地址没有复制到剪贴板。",
		ConfirmText: "确定",
	},
	"updateFailed": {
		Title:       "更新失败",
		Message:     "更新操作没有完成，详细错误信息会显示在这里。",
		ConfirmText: "确定",
	},
	"updateAvailable": {
		Title:       "发现新版本",
		Message:     "检测到新版本。下载更新包不会中断当前直播。",
		ConfirmText: "下载更新",
		CancelText:  "稍后",
	},
	"updateReady": {
		Title:       "更新已下载",
		Message:     "更新包已经下载并解压完成。请选择更新时间。",
		ConfirmText: "立即更新",
		CancelText:  "下次启动时更新",
	},
	"updateProgress": {
		Title:       "BiliQueue 更新",
		Message:     "正在替换程序文件",
		ConfirmText: "后台运行",
	},
	"updateComplete": {
		Title:       "BiliQueue 更新完成",
		Message:     "已更新至新版本，可以打开控制台网页查看。",
		ConfirmText: "确定",
	},
}

func applyTargetDefaults(target string, request DialogRequest) DialogRequest {
	definition, ok := targetDefinitions[target]
	if !ok {
		return request
	}
	if request.Title == "" {
		request.Title = definition.Title
	}
	if request.Message == "" {
		request.Message = definition.Message
	}
	if request.ConfirmText == "" {
		request.ConfirmText = definition.ConfirmText
	}
	if request.CancelText == "" {
		request.CancelText = definition.CancelText
	}
	return request
}
