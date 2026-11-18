package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// The panel key is Ctrl+Shift+Space in these tests; the hook passes these key events.
var ctrlShiftSpace = Hotkey{Mods: ModCtrl | ModShift, VK: 0x20}

const (
	keySpace = 0x20
	keyA     = 'A'
)

// hookEvent is one key transition with the result the matcher must give for it.
type hookEvent struct {
	vk   uint32
	down bool
	want bool
}

func down(vk uint32) hookEvent { return hookEvent{vk: vk, down: true} }
func up(vk uint32) hookEvent   { return hookEvent{vk: vk, down: false} }
func fires(vk uint32) hookEvent {
	return hookEvent{vk: vk, down: true, want: true}
}

func TestComboMatcher(t *testing.T) {
	cases := []struct {
		name   string
		combo  Hotkey
		events []hookEvent
	}{
		{
			name:   "left modifiers then the key fire",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModLControl), down(vkModLShift), fires(keySpace), up(keySpace)},
		},
		{
			name:   "right modifiers fire",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModRControl), down(vkModRShift), fires(keySpace), up(keySpace)},
		},
		{
			name:   "mixed sides and the generic codes fire",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModControl), down(vkModRShift), fires(keySpace), up(keySpace)},
		},
		{
			name:   "the key before the modifiers does not fire, the next press does",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(keySpace), down(vkModLControl), down(vkModLShift), up(keySpace), fires(keySpace)},
		},
		{
			name:   "auto-repeat of the key fires once",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModLControl), down(vkModLShift), fires(keySpace), down(keySpace), down(keySpace), up(keySpace), fires(keySpace)},
		},
		{
			name:   "an extra modifier blocks it",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModLControl), down(vkModLShift), down(vkModLMenu), {vk: keySpace, down: true}},
		},
		{
			name:   "a missing modifier blocks it",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModLControl), {vk: keySpace, down: true}},
		},
		{
			name:   "a modifier released before the key blocks it",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModLControl), down(vkModLShift), up(vkModLShift), {vk: keySpace, down: true}},
		},
		{
			name:   "a Windows key held with the combo blocks it",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModLWin), down(vkModLControl), down(vkModLShift), {vk: keySpace, down: true}},
		},
		{
			name:   "another key with the modifiers does not fire",
			combo:  ctrlShiftSpace,
			events: []hookEvent{down(vkModLControl), down(vkModLShift), {vk: keyA, down: true}},
		},
		{
			name:  "a combo with Alt and a letter",
			combo: Hotkey{Mods: ModAlt, VK: 'C'},
			events: []hookEvent{down(vkModRMenu), fires('C'), up('C'), up(vkModRMenu),
				{vk: 'C', down: true}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m comboMatcher
			for i, ev := range tc.events {
				got := m.feed(keyEvent{VK: ev.vk, Down: ev.down}, tc.combo)
				if got != ev.want {
					t.Fatalf("event %d (vk %#x down=%v) = %v, want %v", i, ev.vk, ev.down, got, ev.want)
				}
			}
		})
	}
}

func TestComboMatcherReleasedModifiersStopTheCombo(t *testing.T) {
	var m comboMatcher
	m.feed(keyEvent{VK: vkModLControl, Down: true}, ctrlShiftSpace)
	m.feed(keyEvent{VK: vkModLShift, Down: true}, ctrlShiftSpace)
	m.feed(keyEvent{VK: vkModLShift, Down: false}, ctrlShiftSpace)
	m.feed(keyEvent{VK: vkModLControl, Down: false}, ctrlShiftSpace)
	m.feed(keyEvent{VK: vkModLShift, Down: true}, ctrlShiftSpace)
	if m.feed(keyEvent{VK: keySpace, Down: true}, ctrlShiftSpace) {
		t.Fatal("Space fired with Ctrl released")
	}
}

func TestComboMatcherIgnoresOutOfRangeCodes(t *testing.T) {
	var m comboMatcher
	if m.feed(keyEvent{VK: 0x1000, Down: true}, ctrlShiftSpace) {
		t.Fatal("an out-of-range virtual key fired")
	}
}

func TestDebouncerAcceptsOncePerWindow(t *testing.T) {
	d := debouncer{window: debounceWindow}
	t0 := time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC)
	steps := []struct {
		after time.Duration
		want  bool
	}{
		{0, true},                       // first press
		{100 * time.Millisecond, false}, // inside the window
		{299 * time.Millisecond, false}, // still inside: a refused press does not restart it
		{300 * time.Millisecond, true},  // 300 ms after the accepted press
		{400 * time.Millisecond, false}, // 100 ms after that one
		{1000 * time.Millisecond, true},
	}
	for i, st := range steps {
		if got := d.allow(t0.Add(st.after)); got != st.want {
			t.Fatalf("press %d at +%v: allow = %v, want %v", i, st.after, got, st.want)
		}
	}
}

func TestDebounceWindowIs300ms(t *testing.T) {
	if debounceWindow != 300*time.Millisecond {
		t.Fatalf("debounceWindow = %v, want 300ms", debounceWindow)
	}
}

func TestHotkeyMethodSwitch(t *testing.T) {
	cases := map[string]string{
		config.HotkeyListen:    config.HotkeyListen,
		config.HotkeyExclusive: config.HotkeyExclusive,
		"":                     config.HotkeyListen,
		"EXCLUSIVO":            config.HotkeyListen, // not a known value: the default
		"otro":                 config.HotkeyListen,
	}
	for in, want := range cases {
		if got := hotkeyMethodFor(in); got != want {
			t.Errorf("hotkeyMethodFor(%q) = %q, want %q", in, got, want)
		}
	}
	if config.Defaults().HotkeyMethod != config.HotkeyListen {
		t.Errorf("default hotkeyMethod = %q, want %q", config.Defaults().HotkeyMethod, config.HotkeyListen)
	}
}

func TestShareNotice(t *testing.T) {
	cases := []struct {
		name     string
		s        Status
		fromTray bool
		since    time.Duration
		want     string
	}{
		{
			name: "the countdown while the addon streams",
			s:    Status{ShareRemainingSec: 15, Bridge: "connected", KeyKnown: true, KeyAgeSec: 1},
			want: "Recibiendo datos del juego… 15s",
		},
		{
			name: "fresh key data after the window",
			s:    Status{Bridge: "no_strip", KeyKnown: true, KeyAgeSec: 42},
			want: "Datos al día (hace 42s)",
		},
		{
			name: "key data older than the fresh limit says nothing",
			s:    Status{Bridge: "no_strip", KeyKnown: true, KeyAgeSec: int64(keyFreshFor/time.Second) + 1},
		},
		{
			name: "no key data yet says nothing",
			s:    Status{Bridge: "no_strip"},
		},
		{
			name:     "opened from the tray with no strip after 2 s: the hint",
			s:        Status{Bridge: "no_strip"},
			fromTray: true,
			since:    2 * time.Second,
			want:     "Pulsa Ctrl+Shift+Espacio dentro del juego para enviar tus datos",
		},
		{
			name:     "opened from the tray, strip not yet after 1 s: no hint yet",
			s:        Status{Bridge: "no_strip"},
			fromTray: true,
			since:    time.Second,
		},
		{
			name:     "opened from the tray with the strip visible: no hint",
			s:        Status{Bridge: "connected"},
			fromTray: true,
			since:    5 * time.Second,
		},
		{
			name:     "the countdown wins over the hint",
			s:        Status{ShareRemainingSec: 3, Bridge: "no_strip"},
			fromTray: true,
			since:    5 * time.Second,
			want:     "Recibiendo datos del juego… 3s",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shareNotice(tc.s, tc.fromTray, tc.since, "Ctrl+Shift+Space"); got != tc.want {
				t.Fatalf("shareNotice = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSpokenKeyUsesSpanishSpace(t *testing.T) {
	if got := spokenKey("Alt+F1"); got != "Alt+F1" {
		t.Errorf("spokenKey(Alt+F1) = %q", got)
	}
	if got := spokenKey("Ctrl+Alt+Space"); got != "Ctrl+Alt+Espacio" {
		t.Errorf("spokenKey(Ctrl+Alt+Space) = %q", got)
	}
}

func TestFormatStatusShowsShareLine(t *testing.T) {
	s := Status{Bridge: "connected", Zone: "Elwynn", Level: 12, SavedVarsAgeSec: 10}
	s.shareLine = "Recibiendo datos del juego… 9s"
	got := formatStatus(s, "", false)
	if !strings.Contains(got, "Recibiendo datos del juego… 9s") {
		t.Fatalf("status %q has no share line", got)
	}
	s.shareLine = ""
	if strings.Contains(formatStatus(s, "", false), "Recibiendo") {
		t.Fatal("share line shown without a share line")
	}
}
