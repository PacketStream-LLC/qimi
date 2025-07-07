# Qimi - Qemu Image Modifier - Interactive

Qimi is a command-line tool that allows you to mount QEMU images (.qcow2, .qcow2c, .raw) and execute commands inside them using chroot.

## Prerequisites

- Linux system with root privileges
- QEMU tools (qemu-nbd)
- Go 1.24.4 or later (for building from source)

## Installation

```bash
go build -o qimi ./cmd/qimi
sudo mv qimi /usr/local/bin/
```

## Usage

### Mount an image

```bash
sudo qimi mount ./image.qcow2 myimage
sudo qimi mount --read-only ./image.qcow2 myimage
```

### List mounted images

```bash
sudo qimi ls
```

### Execute commands in an image

```bash
sudo qimi exec -it myimage /bin/bash
sudo qimi exec --read-only -it ./image.qcow2 /bin/bash
```

### Unmount an image

```bash
sudo qimi unmount myimage
sudo qimi unmount ./image.qcow2
```

### Clean up stale mounts

After a system reboot, mounted images will be lost but may still appear in the list. Clean them up with:

```bash
sudo qimi cleanup
```

## Note

- Qimi requires root privileges to mount images and use chroot
- All data is stored in `/tmp/qimi/` (database and mounts)
- Nothing persists across reboots - clean state every time
- The `cleanup` command can be used to manually remove stale entries during a session