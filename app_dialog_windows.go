//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

type appDialogResult struct {
	Accepted bool   `json:"accepted"`
	Host     string `json:"host"`
	Port     string `json:"port"`
}

type appDialogRequest struct {
	mode        string
	title       string
	message     string
	confirmText string
	cancelText  string
	danger      bool
	host        string
	port        string
	result      chan appDialogResult
}

type persistentAppDialog struct {
	mu         sync.Mutex
	view       webview2.WebView
	hwnd       uintptr
	oldWndProc uintptr
	starting   bool
	ready      chan struct{}
	current    *appDialogRequest
}

var (
	appWebDialogMu          sync.Mutex
	appDialogHost           persistentAppDialog
	appDialogWindowCallback = syscall.NewCallback(appDialogWindowProc)
)

func webView2AvailableQuietly() bool {
	installed, err := webviewloader.GetInstalledVersion()
	return err == nil && strings.TrimSpace(installed) != ""
}

func appWebDialogDataPath() string {
	path := filepath.Join(os.TempDir(), "BiliQueue", "webview2-dialogs")
	_ = os.MkdirAll(path, 0o700)
	return path
}

func promptListenAddress(title, message, defaultValue string) (string, bool) {
	if value, accepted, opened := showWebViewListenDialog(title, message, defaultValue); opened {
		return value, accepted
	}
	return promptListenAddressNative(title, message, defaultValue)
}

func showStyledChoiceDialog(title, message, confirmText, cancelText string) bool {
	if accepted, opened := showWebViewChoiceDialog(title, message, confirmText, cancelText); opened {
		return accepted
	}
	return showStyledChoiceDialogNative(title, message, confirmText, cancelText)
}

func showStyledInfoDialog(title, message string) {
	if showWebViewInfoDialog(title, message, false) {
		return
	}
	showStyledInfoDialogNative(title, message)
}

func showStyledErrorDialog(title, message string) {
	if showWebViewInfoDialog(title, message, true) {
		return
	}
	messageBox(title, message, mbOK|mbIconError|mbSetForeground)
}

func newAppDialogView(title string, width, height int) webview2.WebView {
	if !webView2AvailableQuietly() {
		return nil
	}
	view := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  appWebDialogDataPath(),
		WindowOptions: webview2.WindowOptions{
			Title:  title,
			Width:  uint(width),
			Height: uint(height),
			Center: true,
		},
	})
	if view != nil {
		view.SetSize(width, height, webview2.HintFixed)
	}
	return view
}

func closeActiveWebDialog() {
	appDialogHost.resolve(appDialogResult{})
}

func dialogJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func appDialogDocument(script, body string) string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<style>
*{box-sizing:border-box}html,body{height:100%;margin:0}body{display:flex;flex-direction:column;padding:18px;background:#151923;color:#fff;font-family:"Microsoft YaHei UI","PingFang SC",sans-serif;font-size:13px}h1{margin:0;font-size:18px;line-height:1.35}.head{display:flex;align-items:center;min-height:32px;padding-bottom:14px;border-bottom:1px solid #373f4f}.content{flex:1;min-height:0;padding:18px 0;color:#b8c0cf;line-height:1.75;white-space:pre-wrap;overflow:auto}.actions{display:flex;justify-content:flex-end;gap:12px;padding-top:14px;border-top:1px solid #373f4f}button{height:45px;min-width:108px;padding:0 16px;border:1px solid #3d4658;border-radius:8px;background:#252b37;color:#fff;font:inherit;white-space:nowrap;cursor:pointer}button.primary{border-color:#6577ed;background:#6577ed}button.danger{border-color:#d65d5d;background:#9f3131}.fields{display:grid;grid-template-columns:minmax(0,1fr) 130px;gap:12px;margin-top:14px}.field{display:flex;align-items:center;height:45px;padding:0 12px;border:1px solid #373f4f;border-radius:8px;background:#11141b}.field span{flex:none;margin-right:10px;color:#93999f}.field input{min-width:0;flex:1;border:0;outline:0;background:transparent;color:#fff;font:inherit}.error h1{color:#ff9a9a}
button{display:inline-flex;align-items:center;justify-content:center;line-height:20px;text-align:center}
</style></head><body>` + body + `<script>` + script + `</script></body></html>`
}

func appDialogDocumentPersistent() string {
	body := `<div class="head"><h1 id="title"></h1></div><div class="content"><div id="message"></div><div id="fields" class="fields"><label class="field"><span>地址</span><input id="host" autocomplete="off"></label><label class="field"><span>端口</span><input id="port" inputmode="numeric" autocomplete="off"></label></div></div><div class="actions"><button id="cancel"></button><button id="accept" class="primary"></button></div>`
	script := `const title=document.getElementById('title'),message=document.getElementById('message'),fields=document.getElementById('fields'),host=document.getElementById('host'),port=document.getElementById('port'),accept=document.getElementById('accept'),cancel=document.getElementById('cancel');
window.showBiliQueueDialog=config=>{document.title=config.title;title.textContent=config.title;message.textContent=config.message;title.style.color=config.danger?'#ff9a9a':'#fff';fields.style.display=config.mode==='listen'?'grid':'none';host.value=config.host||'';port.value=config.port||'';accept.textContent=config.confirmText||'确认';cancel.textContent=config.cancelText||'取消';cancel.style.display=config.mode==='info'?'none':'inline-flex';accept.className=config.danger?'danger':'primary';setTimeout(()=>{(config.mode==='listen'?port:accept).focus()},0)};
const resolve=accepted=>window.__bqPersistentDialogResolve(JSON.stringify({accepted,host:host.value,port:port.value}));accept.onclick=()=>resolve(true);cancel.onclick=()=>resolve(false);document.addEventListener('keydown',event=>{if(event.key==='Escape')resolve(false);if(event.key==='Enter'&&document.activeElement===port)resolve(true)});window.__bqPersistentDialogReady();`
	return appDialogDocument(script, body)
}

func preloadAppDialogHost() {
	go appDialogHost.ensure()
}

func (host *persistentAppDialog) ensure() bool {
	if !webView2AvailableQuietly() {
		return false
	}
	host.mu.Lock()
	if host.view != nil && !host.starting {
		host.mu.Unlock()
		return true
	}
	ready := host.ready
	if !host.starting {
		host.starting = true
		ready = make(chan struct{})
		host.ready = ready
		go host.run(ready)
	}
	host.mu.Unlock()
	<-ready
	host.mu.Lock()
	ok := host.view != nil
	host.mu.Unlock()
	return ok
}

func (host *persistentAppDialog) run(ready chan struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	view := newAppDialogView("BiliQueue", 560, 320)
	if view == nil {
		host.finishStart(ready, nil, 0, 0)
		return
	}
	hwnd := uintptr(view.Window())
	miniProcShowWindow.Call(hwnd, miniSWHide)
	var readyOnce sync.Once
	signalReady := func() {
		readyOnce.Do(func() {
			host.mu.Lock()
			host.starting = false
			host.mu.Unlock()
			close(ready)
		})
	}
	if err := view.Bind("__bqPersistentDialogResolve", func(payload string) {
		var result appDialogResult
		_ = json.Unmarshal([]byte(payload), &result)
		host.resolve(result)
	}); err != nil {
		view.Destroy()
		host.finishStart(ready, nil, 0, 0)
		return
	}
	if err := view.Bind("__bqPersistentDialogReady", func() { signalReady() }); err != nil {
		view.Destroy()
		host.finishStart(ready, nil, 0, 0)
		return
	}
	oldWndProc, _, _ := miniProcSetWindowLong.Call(hwnd, miniGWLWndProc, appDialogWindowCallback)
	host.mu.Lock()
	host.view = view
	host.hwnd = hwnd
	host.oldWndProc = oldWndProc
	host.mu.Unlock()
	view.SetHtml(appDialogDocumentPersistent())
	view.Run()
	host.mu.Lock()
	current := host.current
	host.view = nil
	host.hwnd = 0
	host.oldWndProc = 0
	host.starting = false
	host.current = nil
	host.mu.Unlock()
	if current != nil {
		select {
		case current.result <- appDialogResult{}:
		default:
		}
	}
	signalReady()
	view.Destroy()
}

func (host *persistentAppDialog) finishStart(ready chan struct{}, view webview2.WebView, hwnd, oldWndProc uintptr) {
	host.mu.Lock()
	host.view = view
	host.hwnd = hwnd
	host.oldWndProc = oldWndProc
	host.starting = false
	host.mu.Unlock()
	close(ready)
}

func (host *persistentAppDialog) present(request *appDialogRequest) (appDialogResult, bool) {
	if !host.ensure() {
		return appDialogResult{}, false
	}
	host.mu.Lock()
	view := host.view
	hwnd := host.hwnd
	host.current = request
	host.mu.Unlock()
	config, _ := json.Marshal(map[string]any{
		"mode": request.mode, "title": request.title, "message": request.message,
		"confirmText": request.confirmText, "cancelText": request.cancelText,
		"danger": request.danger, "host": request.host, "port": request.port,
	})
	view.Dispatch(func() {
		view.SetTitle(request.title)
		view.Eval("window.showBiliQueueDialog(" + string(config) + ")")
		miniProcShowWindow.Call(hwnd, miniSWRestore)
		miniProcSetForeground.Call(hwnd)
	})
	return <-request.result, true
}

func (host *persistentAppDialog) resolve(result appDialogResult) {
	host.mu.Lock()
	request := host.current
	host.current = nil
	view := host.view
	hwnd := host.hwnd
	host.mu.Unlock()
	if view != nil && hwnd != 0 {
		miniProcShowWindow.Call(hwnd, miniSWHide)
	}
	if request != nil {
		select {
		case request.result <- result:
		default:
		}
	}
}

func appDialogWindowProc(hwnd, message, wParam, lParam uintptr) uintptr {
	appDialogHost.mu.Lock()
	owned := appDialogHost.hwnd == hwnd
	oldWndProc := appDialogHost.oldWndProc
	appDialogHost.mu.Unlock()
	if owned && message == miniWMClose {
		appDialogHost.resolve(appDialogResult{})
		return 0
	}
	if oldWndProc != 0 {
		result, _, _ := miniProcCallWindow.Call(oldWndProc, hwnd, message, wParam, lParam)
		return result
	}
	result, _, _ := miniProcDefWindow.Call(hwnd, message, wParam, lParam)
	return result
}

func showWebViewChoiceDialog(title, message, confirmText, cancelText string) (bool, bool) {
	appWebDialogMu.Lock()
	defer appWebDialogMu.Unlock()
	result, opened := appDialogHost.present(&appDialogRequest{mode: "choice", title: title, message: message, confirmText: confirmText, cancelText: cancelText, result: make(chan appDialogResult, 1)})
	return result.Accepted, opened
}

func showWebViewInfoDialog(title, message string, danger bool) bool {
	appWebDialogMu.Lock()
	defer appWebDialogMu.Unlock()
	_, opened := appDialogHost.present(&appDialogRequest{mode: "info", title: title, message: message, confirmText: "确认", danger: danger, result: make(chan appDialogResult, 1)})
	return opened
}

func showWebViewListenDialog(title, message, defaultValue string) (string, bool, bool) {
	appWebDialogMu.Lock()
	defer appWebDialogMu.Unlock()
	hostValue, portValue := splitListenAddress(defaultValue)
	result, opened := appDialogHost.present(&appDialogRequest{mode: "listen", title: title, message: message, confirmText: "确定", cancelText: "取消", host: hostValue, port: portValue, result: make(chan appDialogResult, 1)})
	if !opened || !result.Accepted {
		return "", false, opened
	}
	hostValue = strings.TrimSpace(result.Host)
	if hostValue == "" {
		hostValue = "127.0.0.1"
	}
	return net.JoinHostPort(hostValue, strings.TrimSpace(result.Port)), true, true
}

func runUpdateHelperProgressWindow(targetVersion string, task func(report func(string, int)) error) error {
	if !webView2AvailableQuietly() {
		return task(func(string, int) {})
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	view := newAppDialogView("BiliQueue 正在更新", 560, 360)
	if view == nil {
		return task(func(string, int) {})
	}
	hwnd := uintptr(view.Window())
	var visible atomic.Bool
	visible.Store(true)
	if err := view.Bind("__bqHideUpdate", func() {
		visible.Store(false)
		miniProcShowWindow.Call(hwnd, miniSWHide)
	}); err != nil {
		view.Destroy()
		return task(func(string, int) {})
	}
	script := fmt.Sprintf(`document.getElementById('version').textContent='v'+%s;document.getElementById('confirm').onclick=()=>window.__bqHideUpdate();`, dialogJSON(targetVersion))
	body := `<div class="head"><h1>正在更新 BiliQueue <span id="version"></span></h1></div><div class="content"><div id="update-stage" style="color:#fff;font-size:15px;margin-bottom:14px">正在启动更新助手</div><div style="height:10px;overflow:hidden;border:1px solid #373f4f;border-radius:5px;background:#11141b"><span id="update-bar" style="display:block;width:8%;height:100%;border-radius:4px;background:#6577ed;transition:width .2s ease"></span></div><div style="margin-top:18px;color:#858d9d;font-size:12px">点击确认或关闭弹窗不影响更新。更新结束后会有消息提醒。</div></div><div class="actions"><button id="confirm" class="primary">确认</button></div>`
	view.SetHtml(appDialogDocument(script, body))
	done := make(chan error, 1)
	go func() {
		report := func(stage string, percent int) {
			if !visible.Load() {
				return
			}
			stageJSON := dialogJSON(stage)
			view.Dispatch(func() {
				view.Eval(fmt.Sprintf(`document.getElementById('update-stage').textContent=%s;document.getElementById('update-bar').style.width='%d%%';`, stageJSON, percent))
			})
		}
		err := task(report)
		if err == nil {
			report("新版本已启动", 100)
			time.Sleep(900 * time.Millisecond)
		}
		done <- err
		view.Dispatch(func() { view.Terminate() })
	}()
	view.Run()
	visible.Store(false)
	err := <-done
	view.Destroy()
	return err
}
