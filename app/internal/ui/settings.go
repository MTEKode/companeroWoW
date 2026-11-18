package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// TipsOnDemandNote is shown under the mode selector while the mode is on demand: the tips are
// disabled, because the app asks the AI only when the player asks.
const TipsOnDemandNote = "En modo bajo demanda la IA solo responde cuando preguntas"

// settingsForm is the settings screen's values as the user left them: text for the
// numbers, so that a bad entry is reported instead of silently replaced.
type settingsForm struct {
	Hotkey        string
	Opacity       string
	FontSize      string
	ProviderOrder []string
	// AntigravityModels is the order in which agy models are tried.
	AntigravityModels []string
	// QuotaCooldownMin is the text of the cooldown minutes (see config.MinQuotaCooldownMin).
	QuotaCooldownMin string
	HideInCombat     bool
	// Mode is how the game is read: config.ModeOnDemand or config.ModeContinuous. "" keeps the base's.
	Mode         string
	TipsMode     string
	TipsSound    bool
	StartWithWoW bool
	WoWPath      string
	// Profile and VerifyAnswers come from the profile screen (see profile_settings.go).
	Profile       config.Profile
	VerifyAnswers bool
}

// formFromConfig fills the form from the saved configuration.
func formFromConfig(c config.Config) settingsForm {
	return settingsForm{
		Hotkey:            c.Hotkey,
		Opacity:           strconv.Itoa(c.Opacity),
		FontSize:          strconv.FormatFloat(c.FontSize, 'f', -1, 64),
		ProviderOrder:     append([]string(nil), c.ProviderOrder...),
		AntigravityModels: append([]string(nil), c.AntigravityModels...),
		QuotaCooldownMin:  strconv.Itoa(c.AntigravityQuotaCooldownMin),
		HideInCombat:      c.HideInCombat,
		Mode:              c.Mode,
		TipsMode:          c.TipsMode,
		TipsSound:         c.TipsSound,
		StartWithWoW:      c.StartWithWoW,
		WoWPath:           c.WoWPath,
		Profile:           c.Profile,
		VerifyAnswers:     c.VerifyAnswers,
	}
}

// checkAntigravityModels accepts the models once each, in the given order. At least one is
// needed, and each must be a slug that agy takes (see config.ValidAntigravityModel).
func checkAntigravityModels(models []string) ([]string, error) {
	var out []string
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" || contains(out, m) {
			continue
		}
		if !config.ValidAntigravityModel(m) {
			return nil, fmt.Errorf("Modelos de Antigravity: %q no es un nombre de modelo válido", m)
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, errors.New("Modelos de Antigravity: deja al menos un modelo")
	}
	return out, nil
}

// checkQuotaCooldown reads the cooldown minutes of the form.
func checkQuotaCooldown(text string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n < config.MinQuotaCooldownMin || n > config.MaxQuotaCooldownMin {
		return 0, fmt.Errorf("Enfriamiento de la cuota: escribe minutos entre %d y %d", config.MinQuotaCooldownMin, config.MaxQuotaCooldownMin)
	}
	return n, nil
}

// knownProviders are the names the provider order may use.
var knownProviders = []string{"antigravity", "codex", "api"}

// apply checks the form and returns base with the form's values in place. Fields the
// screen does not edit (accounts, models, API providers...) keep base's values. The error
// is in Spanish and names the field.
func (f settingsForm) apply(base config.Config) (config.Config, error) {
	hk := strings.TrimSpace(f.Hotkey)
	parsed, err := ParseHotkey(hk)
	if err != nil {
		return config.Config{}, fmt.Errorf("Atajo de teclado: %v", err)
	}
	opacity, err := strconv.Atoi(strings.TrimSpace(f.Opacity))
	if err != nil || opacity < config.MinOpacity || opacity > config.MaxOpacity {
		return config.Config{}, fmt.Errorf("Opacidad: escribe un número entre %d y %d", config.MinOpacity, config.MaxOpacity)
	}
	size, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(f.FontSize), ",", "."), 64)
	if err != nil || size < config.MinFontSize || size > config.MaxFontSize {
		return config.Config{}, fmt.Errorf("Tamaño de letra: escribe un número entre %g y %g", config.MinFontSize, config.MaxFontSize)
	}
	order, err := checkProviderOrder(f.ProviderOrder)
	if err != nil {
		return config.Config{}, err
	}
	models, err := checkAntigravityModels(f.AntigravityModels)
	if err != nil {
		return config.Config{}, err
	}
	cooldown, err := checkQuotaCooldown(f.QuotaCooldownMin)
	if err != nil {
		return config.Config{}, err
	}
	path := strings.TrimSpace(f.WoWPath)
	if path == "" {
		return config.Config{}, errors.New("Carpeta de WoW: indica la carpeta _forever_ o usa Examinar…")
	}
	next := base
	next.Hotkey = parsed.String()
	next.Opacity = opacity
	next.FontSize = size
	next.ProviderOrder = order
	next.AntigravityModels = models
	next.AntigravityQuotaCooldownMin = cooldown
	next.HideInCombat = f.HideInCombat
	if f.Mode != "" {
		if !config.ValidMode(f.Mode) {
			return config.Config{}, fmt.Errorf("Funcionamiento: elige solo al preguntar o continuo")
		}
		next.Mode = f.Mode
	}
	if !config.ValidTipsMode(f.TipsMode) {
		return config.Config{}, fmt.Errorf("Consejos: elige en el panel, notificación discreta o desactivados")
	}
	next.TipsMode = f.TipsMode
	next.TipsSound = f.TipsSound
	next.SyncTips()
	next.StartWithWoW = f.StartWithWoW
	next.Profile = f.Profile.Normalize()
	next.VerifyAnswers = f.VerifyAnswers
	next.WoWPath = path
	return next, nil
}

// checkProviderOrder accepts the providers once each, in the given order.
func checkProviderOrder(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, errors.New("Orden de proveedores: deja al menos un proveedor")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if !contains(knownProviders, n) {
			return nil, fmt.Errorf("Orden de proveedores: %q no es un proveedor conocido", n)
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// settingsChanges names what differs between the saved and the new configuration: the
// settings the running panel applies at once (live) and the ones that need a restart.
func settingsChanges(old, next config.Config) (live, restart []string) {
	if old.Hotkey != next.Hotkey {
		live = append(live, "atajo de teclado")
	}
	if old.Opacity != next.Opacity {
		live = append(live, "opacidad")
	}
	if old.FontSize != next.FontSize {
		live = append(live, "tamaño de letra")
	}
	if old.HideInCombat != next.HideInCombat {
		live = append(live, "ocultar en combate")
	}
	if old.StartWithWoW != next.StartWithWoW {
		live = append(live, "iniciar con WoW")
	}
	if !equalStrings(old.ProviderOrder, next.ProviderOrder) {
		restart = append(restart, "orden de proveedores")
	}
	if !equalStrings(old.AntigravityModels, next.AntigravityModels) {
		restart = append(restart, "modelos de Antigravity")
	}
	if old.AntigravityQuotaCooldownMin != next.AntigravityQuotaCooldownMin {
		restart = append(restart, "enfriamiento de la cuota")
	}
	if old.TipsMode != next.TipsMode {
		live = append(live, "consejos")
	}
	if old.TipsSound != next.TipsSound {
		live = append(live, "sonido de los consejos")
	}
	if old.WoWPath != next.WoWPath {
		restart = append(restart, "carpeta de WoW")
	}
	live = append(live, profileChangeText(old.Profile, next.Profile, old.VerifyAnswers, next.VerifyAnswers)...)
	return live, restart
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// saveMessage is the line under the settings buttons after a save.
func saveMessage(live, restart []string) string {
	var parts []string
	if len(live) > 0 {
		parts = append(parts, "Aplicado: "+strings.Join(live, ", "))
	}
	if len(restart) > 0 {
		parts = append(parts, "Se aplicará al reiniciar: "+strings.Join(restart, ", "))
	}
	if len(parts) == 0 {
		return "Sin cambios"
	}
	return strings.Join(parts, ". ")
}

// moveItem moves the item at index i one place up (dir -1) or down (dir +1) and returns the
// new list and the new index. Out-of-range moves return the list unchanged.
func moveItem(list []string, i, dir int) ([]string, int) {
	j := i + dir
	if i < 0 || i >= len(list) || j < 0 || j >= len(list) {
		return list, i
	}
	out := append([]string(nil), list...)
	out[i], out[j] = out[j], out[i]
	return out, j
}

// keyStatusText says whether the NVIDIA key resolves, never its value.
func keyStatusText(configured bool) string {
	if configured {
		return "Clave de NVIDIA: configurada"
	}
	return "Clave de NVIDIA: no configurada"
}
