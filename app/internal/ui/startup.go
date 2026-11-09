package ui

// startArg is the command-line switch that starts the app minimized to the tray.
const startArg = "--tray"

// runEntry is the value written to the HKCU Run key: the executable, quoted, and the tray
// switch. It is quoted because the install folder may contain spaces.
func runEntry(exe string) string {
	return `"` + exe + `" ` + startArg
}
