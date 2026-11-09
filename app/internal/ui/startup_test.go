package ui

import "testing"

func TestRunEntryQuotesPathAndStartsHidden(t *testing.T) {
	got := runEntry(`C:\Program Files\Compañero\CompaneroWoW.exe`)
	want := `"C:\Program Files\Compañero\CompaneroWoW.exe" --tray`
	if got != want {
		t.Errorf("runEntry = %s, want %s", got, want)
	}
}
