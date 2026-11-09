// Package ui is the overlay panel: a native Win32 window (no browser engine) shown on top
// of WoW and toggled by a global hotkey. The types, the window state file, the Markdown
// conversion and the layout arithmetic are platform-neutral and tested everywhere; only
// the window itself is Windows-specific.
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/assistant"
	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// WindowTitle is the caption of the overlay window (also used to find a running copy).
const WindowTitle = "Compañero WoW"

// Backend is what the window asks of the assistant.
type Backend interface {
	Ask(ctx context.Context, question string, withScreenshot bool) (ai.Response, error)
	History(n int) ([]store.ChatMsg, error)
	ClearHistory() error
	// SessionSummary is the short summary of the play session (no AI call).
	SessionSummary() (string, error)
}

// Status is the state line shown at the bottom of the window. It is refreshed every 2 s
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
	// Channel is the live data channel line ("Canal en vivo: OK (1.2 KB/s)"); empty when unknown.
	Channel string `json:"channel,omitempty"`
}

// Options configures Run.
type Options struct {
	// Hotkey is the global shortcut, for example "Ctrl+Shift+Space".
	Hotkey string
	// StatePath is the window.json file that remembers position and size.
	StatePath string
	Backend   Backend
	// Status is called on a timer while the window is visible, and when it is shown.
	Status func() Status
	// Tips delivers proactive tips; Run shows each one in the conversation without
	// taking the focus.
	Tips <-chan assistant.Tip
	// Avoid returns the screen rectangle the panel must never cover: the WoW pixel strip
	// at the top-left of the client area. ok is false when WoW is not found. It may be
	// called from any goroutine.
	Avoid func() (r Rect, ok bool)

	// Config holds the settings the settings screen edits. The panel keeps its own copy
	// and writes it to ConfigPath when the user saves; Hotkey above must match Config.Hotkey.
	Config     config.Config
	ConfigPath string
	// InCombat reports whether the character is in combat, from the live bridge. It may be
	// called from any goroutine. It is polled every half second while Config.HideInCombat
	// is set.
	InCombat func() bool
	// Feedback keeps the 👍/👎 ratings of the answers. Nil hides the buttons.
	Feedback FeedbackSaver
	// StartHidden starts with the panel hidden: only the tray icon shows (--tray).
	StartHidden bool
	// Setup opens the first-run wizard in the panel (a new config.json, or --setup).
	Setup bool
	// DataDir is the app's data folder; ExeDir is the folder of the executable, where an
	// addon copy may ship (addon\CompaneroWoW).
	DataDir string
	ExeDir  string
}

// windowState is the saved geometry of the window, in physical pixels.
type windowState struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Logical sizes (at 96 DPI). The window is scaled by the monitor DPI.
const (
	defaultWidth  = 420
	defaultHeight = 560
	minWidth      = 300
	minHeight     = 300
)

// defaultState is used when window.json is missing or unusable.
func defaultState(scale float64) windowState {
	return windowState{W: scaled(defaultWidth, scale), H: scaled(defaultHeight, scale)}
}

// loadWindowState reads the saved geometry. found is false when there is nothing
// usable, in which case the caller places the window at its default spot.
func loadWindowState(path string) (s windowState, found bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return windowState{}, false
	}
	var saved windowState
	if err := json.Unmarshal(data, &saved); err != nil {
		return windowState{}, false
	}
	if saved.W < minWidth || saved.H < minHeight {
		return windowState{}, false
	}
	return saved, true
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

// scaled converts a logical size to pixels at the given scale (1.0 = 96 DPI).
func scaled(v int, scale float64) int {
	return int(math.Round(float64(v) * scale))
}

// safeURL allows only http and https links to be opened.
func safeURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if !isHTTPURL(u) {
		return "", fmt.Errorf("enlace no permitido: solo http y https")
	}
	if strings.ContainsAny(u, " \t\r\n\"<>") {
		return "", fmt.Errorf("enlace no válido")
	}
	return u, nil
}

// answerMsg is one history entry as the window reads it.
type answerMsg struct {
	Role     string
	Content  string
	Provider string
	At       string
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

// noticeTTL is how long a transient notice (a copy result, a failed action) stays in the
// status line before it clears.
const noticeTTL = 5 * time.Second

// noticeExpired reports whether a notice set at `at` has been shown for noticeTTL.
func noticeExpired(at, now time.Time) bool {
	return !at.IsZero() && now.Sub(at) >= noticeTTL
}

// askError turns an assistant error into the one-line Spanish text shown to the user.
// The full error chain is written to the log.
func askError(err error) string {
	if err == nil {
		return ""
	}
	log.Printf("ui: pregunta sin respuesta: %v", err)
	return friendlyError(err)
}

// friendlyError is the user-facing summary of an assistant error.
func friendlyError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "La respuesta tardó demasiado (más de 2 min 30 s). Inténtalo de nuevo."
	case errors.Is(err, context.Canceled):
		return "Pregunta cancelada."
	case strings.Contains(err.Error(), "ningún proveedor de IA respondió"):
		return "No respondió ningún proveedor; revisa que hayas iniciado sesión en agy/codex o la clave de NVIDIA."
	case strings.Contains(err.Error(), "no hay proveedores de IA configurados"):
		return "No hay proveedores de IA configurados; revisa providerOrder y apiProviders en config.json."
	}
	return "No pude responder. El detalle está en companero.log."
}
