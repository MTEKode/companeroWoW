// Package ui is the overlay window: a WebView2 page (embedded HTML, CSS and JS, no
// network) shown on top of WoW, toggled by a global hotkey. Only the window itself is
// Windows-specific; the types, the window state file and the page assembly are not.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// WindowTitle is the caption of the overlay window.
const WindowTitle = "Compañero WoW"

// WebView2DownloadURL is where the user can get the WebView2 runtime.
const WebView2DownloadURL = "https://developer.microsoft.com/microsoft-edge/webview2/"

// ErrRuntimeMissing means the WebView2 runtime is not installed. The window shows a
// message box with the download link before this error is returned.
var ErrRuntimeMissing = errors.New("el runtime de WebView2 no está instalado")

//go:embed web
var webFS embed.FS

// Backend is what the window asks of the assistant.
type Backend interface {
	Ask(ctx context.Context, question string, withScreenshot bool) (ai.Response, error)
	History(n int) ([]store.ChatMsg, error)
	ClearHistory() error
}

// Status is the state line shown at the bottom of the window. It is pushed every 2 s
// while the window is visible.
type Status struct {
	// Bridge is "connected" (pixel strip read), "no_wow" (client not found) or
	// "no_strip" (client found but the strip is not readable).
	Bridge string `json:"bridge"`
	Zone   string `json:"zone,omitempty"`
	Level  int    `json:"level,omitempty"`
	// SavedVarsAgeSec is the age of the SavedVariables file, or -1 when unknown.
	SavedVarsAgeSec int64    `json:"savedVarsAgeSec"`
	Hints           []string `json:"hints"`
	Notice          string   `json:"notice,omitempty"`
	Providers       []string `json:"providers"`
}

// Options configures Run.
type Options struct {
	// Hotkey is the global shortcut, for example "Ctrl+Shift+Space".
	Hotkey string
	// StatePath is the window.json file that remembers position and size.
	StatePath string
	// DataPath is the WebView2 user data folder.
	DataPath string
	Backend  Backend
	// Status is called on the push timer and when the window is shown.
	Status func() Status
	// Tips delivers proactive tips; Run shows each one as a card.
	Tips <-chan assistant.Tip
}

// windowState is the saved geometry of the window.
type windowState struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

const (
	defaultWidth  = 420
	defaultHeight = 620
	minWidth      = 320
	minHeight     = 420
)

// defaultState is used when window.json is missing or unusable.
func defaultState() windowState {
	return windowState{W: defaultWidth, H: defaultHeight}
}

// loadWindowState reads the saved geometry. Anything unusable falls back to the default.
func loadWindowState(path string) windowState {
	s := defaultState()
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var saved windowState
	if err := json.Unmarshal(data, &saved); err != nil {
		return s
	}
	if saved.W >= minWidth && saved.H >= minHeight {
		s.W, s.H = saved.W, saved.H
	}
	s.X, s.Y = saved.X, saved.Y
	return s
}

// saveWindowState writes the geometry atomically. Errors are returned but not fatal.
func saveWindowState(path string, s windowState) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode window state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create window state dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write window state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace window state: %w", err)
	}
	return nil
}

// pageHTML assembles the embedded page: the stylesheet and script are inlined so the
// page needs nothing from the network.
func pageHTML() (string, error) {
	page, err := webFS.ReadFile("web/index.html")
	if err != nil {
		return "", fmt.Errorf("read embedded page: %w", err)
	}
	css, err := webFS.ReadFile("web/app.css")
	if err != nil {
		return "", fmt.Errorf("read embedded stylesheet: %w", err)
	}
	js, err := webFS.ReadFile("web/app.js")
	if err != nil {
		return "", fmt.Errorf("read embedded script: %w", err)
	}
	html := string(page)
	html = strings.Replace(html, "/*CSS*/", string(css), 1)
	html = strings.Replace(html, "/*JS*/", string(js), 1)
	return html, nil
}

// safeURL allows only http and https links to be opened.
func safeURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	lower := strings.ToLower(u)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return "", fmt.Errorf("enlace no permitido: solo http y https")
	}
	if strings.ContainsAny(u, " \t\r\n\"<>") {
		return "", fmt.Errorf("enlace no válido")
	}
	return u, nil
}

// answerMsg is one history entry as the page reads it.
type answerMsg struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Provider string `json:"provider"`
	At       string `json:"at"`
}

func toAnswerMsgs(in []store.ChatMsg) []answerMsg {
	out := make([]answerMsg, 0, len(in))
	for _, m := range in {
		out = append(out, answerMsg{
			Role:     m.Role,
			Content:  m.Content,
			Provider: m.Provider,
			At:       m.CreatedAt.Format("15:04"),
		})
	}
	return out
}

// answerPayload is the value resolved for a question.
type answerPayload struct {
	Text     string `json:"text"`
	Provider string `json:"provider"`
}

// askError turns an assistant error into the text shown to the user.
func askError(err error) string {
	if err == nil {
		return ""
	}
	return "No pude responder: " + err.Error()
}
