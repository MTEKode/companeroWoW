package ui

import "testing"

func TestParseHotkeyTable(t *testing.T) {
	cases := []struct {
		in      string
		want    Hotkey
		wantStr string
		wantErr bool
	}{
		{in: "Ctrl+Shift+Space", want: Hotkey{Mods: ModCtrl | ModShift, VK: 0x20}, wantStr: "Ctrl+Shift+Space"},
		{in: "Alt+F1", want: Hotkey{Mods: ModAlt, VK: 0x70}, wantStr: "Alt+F1"},
		{in: "alt + f12", want: Hotkey{Mods: ModAlt, VK: 0x7B}, wantStr: "Alt+F12"},
		{in: "CTRL+ALT+F24", want: Hotkey{Mods: ModCtrl | ModAlt, VK: 0x87}, wantStr: "Ctrl+Alt+F24"},
		{in: "Win+Shift+A", want: Hotkey{Mods: ModWin | ModShift, VK: 'A'}, wantStr: "Shift+Win+A"},
		{in: "Control+Windows+7", want: Hotkey{Mods: ModCtrl | ModWin, VK: '7'}, wantStr: "Ctrl+Win+7"},
		{in: "Ctrl+Enter", want: Hotkey{Mods: ModCtrl, VK: 0x0D}, wantStr: "Ctrl+Enter"},
		{in: "Shift+Tab", want: Hotkey{Mods: ModShift, VK: 0x09}, wantStr: "Shift+Tab"},
		{in: "Ctrl+Esc", want: Hotkey{Mods: ModCtrl, VK: 0x1B}, wantStr: "Ctrl+Esc"},
		{in: "F5", want: Hotkey{VK: 0x74}, wantStr: "F5"},
		{in: "Ctrl+z", want: Hotkey{Mods: ModCtrl, VK: 'Z'}, wantStr: "Ctrl+Z"},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "Ctrl+", wantErr: true},
		{in: "Ctrl+Shift", wantErr: true},
		{in: "Ctrl+A+B", wantErr: true},
		{in: "Ctrl+F25", wantErr: true},
		{in: "Ctrl+F0", wantErr: true},
		{in: "Ctrl+Pause", wantErr: true},
		{in: "Ctrl++A", wantErr: true},
		{in: "Super+A", wantErr: true},
	}
	for _, tc := range cases {
		got, err := ParseHotkey(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseHotkey(%q) = %+v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseHotkey(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseHotkey(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
		if s := got.String(); s != tc.wantStr {
			t.Errorf("ParseHotkey(%q).String() = %q, want %q", tc.in, s, tc.wantStr)
		}
	}
}
