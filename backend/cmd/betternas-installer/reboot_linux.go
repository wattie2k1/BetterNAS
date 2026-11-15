package main

import "syscall"

// reboot startet den Rechner direkt über den Kernel neu. Das funktioniert auch, wenn der
// USB-Stick (und damit das Live-Dateisystem) bereits entfernt wurde – systemctl wäre dann
// nicht mehr lesbar. Das Zielsystem ist zu diesem Zeitpunkt bereits ausgehängt.
func reboot() error {
	syscall.Sync()
	return syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}
