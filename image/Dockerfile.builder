FROM debian:trixie
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      mmdebstrap live-build debootstrap squashfs-tools xorriso zstd \
      dosfstools mtools e2fsprogs file ca-certificates shellcheck cpio apt-utils gnupg \
      grub-efi-amd64-bin grub-pc-bin isolinux syslinux-common syslinux-utils \
    && rm -rf /var/lib/apt/lists/*
# dash kann keine Dateideskriptoren >= 10 umleiten (unter Rosetta belegt); apt ruft "sh -c 'cat >&12'" auf.
RUN ln -sf bash /usr/bin/sh
COPY chroot-wrapper.sh /usr/local/sbin/chroot-wrapper
RUN mv /usr/sbin/chroot /usr/sbin/chroot.real && install -m 0755 /usr/local/sbin/chroot-wrapper /usr/sbin/chroot
