//go:build windows

package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	webview "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	shell32                 = windows.NewLazySystemDLL("shell32.dll")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procShowWindow          = user32.NewProc("ShowWindow")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtrW   = user32.NewProc("GetWindowLongPtrW")
	procCallWindowProcW     = user32.NewProc("CallWindowProcW")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procShellExecuteW       = shell32.NewProc("ShellExecuteW")
)

const (
	swHide           = 0
	swShow           = 5
	swShowNoActivate = 4
	swRestore        = 9

	hwndTopmost   = ^uintptr(0) // (HWND)-1
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoActivate = 0x0010

	gwlpWndProc = ^uintptr(3) // GWLP_WNDPROC (-4)
	wmClose     = 0x0010

	smCxScreen = 0
	smCyScreen = 1

	mbOK        = 0x00000000
	mbIconError = 0x00000010
)

// window is the overlay. Only the UI thread touches wv and the saved geometry; other
// goroutines hand work over with wv.Dispatch.
type window struct {
	wv       webview.WebView
	hwnd     uintptr
	opts     Options
	state    windowState
	quitting atomic.Bool

	mu     sync.Mutex
	notice string // hotkey or other notice shown in the status bar
}

// current is the single overlay window. The window procedure is a plain function, so it
// finds the window through this variable.
var (
	current  *window
	origProc uintptr
)

// Run creates the overlay window and runs its message loop on the calling thread,
// which must be the main thread. It returns when the user quits (or on error). Closing
// the window only hides it. If WebView2 is missing, a message box is shown and
// ErrRuntimeMissing is returned.
func Run(opts Options) error {
	runtime.LockOSThread()

	if err := checkRuntime(); err != nil {
		showMessage("Compañero WoW necesita Microsoft Edge WebView2 Runtime, que no está instalado.\n\n"+
			"Descárgalo e instálalo desde:\n"+WebView2DownloadURL+"\n\n"+
			"Después vuelve a abrir la aplicación.", "Compañero WoW")
		return ErrRuntimeMissing
	}
	html, err := pageHTML()
	if err != nil {
		return err
	}

	state := loadWindowState(opts.StatePath)
	wv := webview.NewWithOptions(webview.WebViewOptions{
		DataPath: opts.DataPath,
		WindowOptions: webview.WindowOptions{
			Title:  WindowTitle,
			Width:  uint(state.W),
			Height: uint(state.H),
		},
	})
	if wv == nil {
		return errors.New("no pude crear la ventana de WebView2")
	}

	win := &window{wv: wv, hwnd: uintptr(wv.Window()), opts: opts, state: state}
	current = win
	win.placeWindow()
	win.subclass()

	if err := win.bind(); err != nil {
		return err
	}
	wv.SetHtml(html)

	win.registerHotkey()
	win.show(true)

	go win.statusLoop()
	go win.tipLoop()

	wv.Run()

	win.saveGeometry()
	return nil
}

// Activate brings an already running overlay to the front. It reports whether one was found.
func Activate() bool {
	title, err := windows.UTF16PtrFromString(WindowTitle)
	if err != nil {
		return false
	}
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if h == 0 {
		return false
	}
	procShowWindow.Call(h, swRestore)
	procSetForegroundWindow.Call(h)
	return true
}

// checkRuntime reports whether the WebView2 runtime is installed.
func checkRuntime() error {
	version, err := webviewloader.GetInstalledVersion()
	if err != nil {
		return err
	}
	if strings.TrimSpace(version) == "" {
		return ErrRuntimeMissing
	}
	return nil
}

func showMessage(text, caption string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(caption)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), mbOK|mbIconError)
}

// placeWindow applies the saved geometry, or a default spot at the top right of the
// screen, and keeps the window always on top.
func (w *window) placeWindow() {
	sw, _, _ := procGetSystemMetrics.Call(smCxScreen)
	sh, _, _ := procGetSystemMetrics.Call(smCyScreen)
	s := w.state
	if (s.X == 0 && s.Y == 0) || s.X < 0 || s.Y < 0 || int(sw) < s.X+64 || int(sh) < s.Y+64 {
		s.X = int(sw) - s.W - 24
		if s.X < 0 {
			s.X = 0
		}
		s.Y = 80
	}
	w.state = s
	procSetWindowPos.Call(w.hwnd, hwndTopmost, uintptr(s.X), uintptr(s.Y), uintptr(s.W), uintptr(s.H), swpNoActivate)
}

// subclass replaces the window procedure with one that hides the window on close and
// passes every other message to the library's procedure.
func (w *window) subclass() {
	orig, _, _ := procGetWindowLongPtrW.Call(w.hwnd, gwlpWndProc)
	origProc = orig
	proc := windows.NewCallback(overlayProc)
	procSetWindowLongPtrW.Call(w.hwnd, gwlpWndProc, proc)
}

func overlayProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == wmClose && current != nil && !current.quitting.Load() {
		current.hide()
		return 0
	}
	r, _, _ := procCallWindowProcW.Call(origProc, hwnd, msg, wp, lp)
	return r
}

func (w *window) visible() bool {
	r, _, _ := procIsWindowVisible.Call(w.hwnd)
	return r != 0
}

// show makes the window visible. Focus is taken only when focus is true, so that tips
// do not steal the keyboard from the game.
func (w *window) show(focus bool) {
	if focus {
		procShowWindow.Call(w.hwnd, swShow)
		procSetWindowPos.Call(w.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize)
		procSetForegroundWindow.Call(w.hwnd)
		w.wv.Eval("window.companeroFocus&&companeroFocus()")
	} else {
		procShowWindow.Call(w.hwnd, swShowNoActivate)
		procSetWindowPos.Call(w.hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
	}
	w.pushStatusNow()
}

func (w *window) hide() {
	w.saveGeometry()
	procShowWindow.Call(w.hwnd, swHide)
}

// toggle is the hotkey action: hide when visible, show with focus otherwise.
func (w *window) toggle() {
	if w.visible() {
		w.hide()
		return
	}
	w.show(true)
}

func (w *window) saveGeometry() {
	var r struct{ Left, Top, Right, Bottom int32 }
	if ok, _, _ := procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return
	}
	s := windowState{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
	if s.W < minWidth || s.H < minHeight {
		return
	}
	w.state = s
	if err := saveWindowState(w.opts.StatePath, s); err != nil {
		log.Printf("ui: save window state: %v", err)
	}
}

func (w *window) registerHotkey() {
	hk, err := ParseHotkey(w.opts.Hotkey)
	if err != nil {
		w.setNotice(fmt.Sprintf("Atajo no válido en config.json: %v", err))
		return
	}
	if err := startHotkey(hk, func() {
		w.wv.Dispatch(w.toggle)
	}); err != nil {
		log.Printf("ui: hotkey %s: %v", hk, err)
		w.setNotice(fmt.Sprintf("No pude registrar el atajo %s: quizá ya lo usa otra aplicación. Puedes cambiarlo en config.json.", hk))
	}
}

func (w *window) setNotice(s string) {
	w.mu.Lock()
	w.notice = s
	w.mu.Unlock()
}

func (w *window) getNotice() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.notice
}

// status returns the status with the window notice merged in.
func (w *window) status() Status {
	var s Status
	if w.opts.Status != nil {
		s = w.opts.Status()
	}
	if s.Hints == nil {
		s.Hints = []string{}
	}
	if s.Providers == nil {
		s.Providers = []string{}
	}
	if n := w.getNotice(); n != "" {
		if s.Notice != "" {
			s.Notice += " · "
		}
		s.Notice += n
	}
	return s
}

// pushStatusNow sends the status to the page (UI thread).
func (w *window) pushStatusNow() {
	if w.opts.Status == nil {
		return
	}
	js, err := json.Marshal(w.status())
	if err != nil {
		return
	}
	w.wv.Eval("window.companeroStatus&&companeroStatus(" + string(js) + ")")
}

// statusLoop pushes the status every two seconds while the window is visible.
func (w *window) statusLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !w.visible() {
			continue
		}
		s := w.status()
		js, err := json.Marshal(s)
		if err != nil {
			continue
		}
		w.wv.Dispatch(func() {
			if w.visible() {
				w.wv.Eval("window.companeroStatus&&companeroStatus(" + string(js) + ")")
			}
		})
	}
}

// tipLoop shows each proactive tip. A hidden window is shown without taking focus.
func (w *window) tipLoop() {
	if w.opts.Tips == nil {
		return
	}
	for tip := range w.opts.Tips {
		js, err := json.Marshal(map[string]string{"title": tip.Title, "text": tip.Text})
		if err != nil {
			continue
		}
		w.wv.Dispatch(func() {
			if !w.visible() {
				w.show(false)
			}
			w.wv.Eval("window.companeroTip&&companeroTip(" + string(js) + ")")
		})
	}
}

// bind exposes the Go functions the page calls. Each runs on the UI thread, except
// uiAsk, which runs the question in the background and answers through Eval.
func (w *window) bind() error {
	bindings := map[string]any{
		"uiHistory":      w.uiHistory,
		"uiClearHistory": w.uiClearHistory,
		"uiStatus":       w.status,
		"uiOpenURL":      w.uiOpenURL,
		"uiQuit":         w.uiQuit,
		"uiAsk":          w.uiAsk,
	}
	for name, fn := range bindings {
		if err := w.wv.Bind(name, fn); err != nil {
			return fmt.Errorf("bind %s: %w", name, err)
		}
	}
	return nil
}

func (w *window) uiHistory() ([]answerMsg, error) {
	if w.opts.Backend == nil {
		return []answerMsg{}, nil
	}
	msgs, err := w.opts.Backend.History(60)
	if err != nil {
		return nil, err
	}
	return toAnswerMsgs(msgs), nil
}

func (w *window) uiClearHistory() error {
	if w.opts.Backend == nil {
		return nil
	}
	return w.opts.Backend.ClearHistory()
}

func (w *window) uiOpenURL(raw string) error {
	u, err := safeURL(raw)
	if err != nil {
		return err
	}
	op, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(u)
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(op)), uintptr(unsafe.Pointer(target)), 0, 0, swShow)
	if r <= 32 {
		return fmt.Errorf("no pude abrir el enlace en el navegador")
	}
	return nil
}

// uiQuit closes the window for good; wv.Run returns afterwards.
func (w *window) uiQuit() {
	w.quitting.Store(true)
	w.saveGeometry()
	w.wv.Destroy()
}

// uiAsk starts a question in the background. The answer is delivered with
// companeroAnswer(id, ok, payload) on the UI thread.
func (w *window) uiAsk(id int, text string, withShot bool) {
	go func() {
		payload, ok := w.answer(text, withShot)
		js, err := json.Marshal(payload)
		if err != nil {
			js = []byte(`"error"`)
		}
		w.wv.Dispatch(func() {
			w.wv.Eval(fmt.Sprintf("window.companeroAnswer(%d, %t, %s)", id, ok, js))
		})
	}()
}

// answer runs the question and returns either an answerPayload (ok) or an error text.
func (w *window) answer(text string, withShot bool) (any, bool) {
	if w.opts.Backend == nil {
		return askError(errors.New("el asistente no está disponible")), false
	}
	resp, err := w.opts.Backend.Ask(context.Background(), text, withShot)
	if err != nil {
		return askError(err), false
	}
	return answerPayload{Text: resp.Text, Provider: resp.Provider}, true
}

// RuntimeInstalled reports whether the WebView2 runtime is available.
func RuntimeInstalled() bool {
	return checkRuntime() == nil
}
