//go:build windows

package config

import (
	"golang.org/x/sys/windows/registry"
)

// getMachineID returns the Windows MachineGuid from registry.
// HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid
func getMachineID() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()

	val, _, err := key.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return val
}
