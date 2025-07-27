# qimi - QEMU Image Manipulator

> [!WARNING]  
> This is not an official PacketStream LLC service or product.

**qimi** is a command-line tool that allows you to mount QEMU disk images (`.qcow2`, `.qcow2c`, `.raw`) and execute commands inside them using chroot.

## Prerequisites

- Linux system with root privileges
- QEMU tools (`qemu-nbd`)
- Go 1.24.4 or later (for building from source)

## Installation

Build from source:

```bash
go build -o qimi ./cmd/qimi
```

## Usage Overview

qimi supports two mounting strategies:

| Mode | Description | Best For |
|------|-------------|----------|
| **Persistent Mount** | Mount once, use multiple times | Running several commands on the same image |
| **Temporary Mount** | Auto-mount and unmount for single commands | One-off operations |

## Persistent Mounts

Use persistent mounts when you need to run multiple commands on the same image. This prevents unnecessary remounting overhead.

### Mount an Image

```bash
sudo qimi mount ./image.qcow2 myimage
```

This mounts `image.qcow2` with the alias `myimage`. The mount remains active until you unmount it or reboot.

### List Active Mounts

```bash
sudo qimi ls
```

### Execute Commands

```bash
sudo qimi exec -it myimage /bin/bash
```

### Unmount When Done

```bash
sudo qimi unmount myimage
```

## Temporary Mounts

For quick, one-time operations, use temporary mounts. qimi automatically handles mounting and unmounting:

```bash
sudo qimi exec -it ./image.qcow2 /bin/bash
```

The image is automatically unmounted when the command completes.

## Examples

### Interactive Shell Session (Persistent)
```bash
# Mount the image
sudo qimi mount ./ubuntu-server.qcow2 ubuntu

# Start interactive session
sudo qimi exec -it ubuntu /bin/bash

# When done, unmount
sudo qimi unmount ubuntu
```

### Quick Package Installation (Temporary)
```bash
# One-time command execution
sudo qimi exec ./debian.qcow2 apt-get update
```

### Check What's Mounted
```bash
sudo qimi ls
```

## Troubleshooting

### Clean Up Stale Mounts

After a system reboot, mounted images are lost but may still appear in the mount list due to tmpfs implementation issues. Clean them up with:

```bash
sudo qimi cleanup
```

## Command Reference

| Command | Description |
|---------|-------------|
| `qimi mount <image> <name>` | Create a persistent mount |
| `qimi unmount <name>` | Remove a persistent mount |
| `qimi ls` | List all active mounts |
| `qimi exec [options] <image/name> <command>` | Execute command in mounted image |
| `qimi cleanup` | Remove stale mount entries |

### exec Options
- `-i` - Interactive mode
- `-t` - Allocate a TTY
