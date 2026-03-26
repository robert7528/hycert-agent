//go:build linux

package config

import (
	"os"
	"strings"
)

// getMachineID returns the Linux machine-id.
// Primary: /etc/machine-id (systemd)
// Fallback: /var/lib/dbus/machine-id (older distros)
func getMachineID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := os.ReadFile(path)
		if err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				return id
			}
		}
	}
	return ""
}
