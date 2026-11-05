package autostart

import (
	"os"
	"path/filepath"
	"strings"
)

func path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", Label+".desktop"), nil
}

// entry is an XDG autostart entry, which GNOME, KDE and XFCE all read.
func entry(exe string) string {
	return `[Desktop Entry]
Type=Application
Name=doze
Comment=doze in the menu bar
Exec=` + quote(exe) + ` open
Icon=doze
Terminal=false
X-GNOME-Autostart-enabled=true
`
}

// quote is the Exec key's quoting: double quotes around an argument with
// a space, and the four characters it reserves escaped.
func quote(s string) string {
	if !strings.ContainsAny(s, " \t\"`$\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", "$", `\$`).Replace(s) + `"`
}
