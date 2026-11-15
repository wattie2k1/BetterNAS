#!/bin/sh
# Rosetta (amd64 auf Apple Silicon) braucht /proc/self/exe – auch innerhalb eines chroot.
# debootstrap ruft chroot auf, bevor /proc eingehängt ist. Dieser Wrapper hängt /proc dafür
# vorübergehend ein und danach wieder aus. Auf nativen Systemen ist er wirkungslos.
target="${1:-}"
mounted=""
case "$target" in
-*|"") ;;
*)
	if [ -d "$target/proc" ] && ! mountpoint -q "$target/proc"; then
		mount -t proc proc "$target/proc" && mounted=1
	fi
	;;
esac
/usr/sbin/chroot.real "$@"
rc=$?
[ -n "$mounted" ] && umount -l "$target/proc"
exit $rc
