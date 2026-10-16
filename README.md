<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="branding/logo-inverse.svg">
    <img src="branding/logo.svg" alt="AsyncOS" width="248">
  </picture>
</p>

<p align="center">A lean, appliance-grade Linux operating system for single-board computers, mini PCs and virtual machines.</p>

---

AsyncOS is a minimal Linux distribution built with [Buildroot](https://buildroot.org/). It is not based on a general-purpose distribution like Debian or Ubuntu: every image is assembled from source, boots from a read-only root file system, and updates itself atomically over the air.

## Features

- **Small and fast** – a read-only EROFS root file system and ZRAM-backed `/tmp`, `/var` and swap keep memory use and disk I/O low.
- **Atomic A/B updates** – [RAUC](https://rauc.io/) installs updates to the inactive slot and falls back automatically if a new version fails to boot. Offline updates via USB work too.
- **Container native** – workloads run as containers on [Docker Engine](https://docs.docker.com/engine/).
- **Secure by default** – [AppArmor](https://apparmor.net/) confinement and signed update bundles.

## Supported hardware

| Board ID          | Hardware                                    |
| ----------------- | ------------------------------------------- |
| `generic-x86-64`  | UEFI-capable x86-64 PCs                     |
| `generic-aarch64` | UEFI-capable aarch64 systems                |
| `ova`             | Virtual machines (OVA, qcow2, VDI, VHDX, VMDK) |
| `rpi3-64`, `rpi4-64`, `rpi5-64` | Raspberry Pi 3, 4/400, 5      |
| `odroid-c2`, `odroid-c4`, `odroid-n2`, `odroid-m1`, `odroid-m1s` | Hardkernel ODROID |
| `khadas-vim3`     | Khadas VIM3                                 |
| `green`, `yellow` | Home Assistant Green and Yellow hardware    |

Kernel versions per board are listed in [Documentation/kernel.md](Documentation/kernel.md).

## Building

Builds run inside a container so the only host requirement is Docker.

```bash
git clone --recurse-submodules https://github.com/the-asad-iqbal/async-os.git
cd async-os
scripts/enter.sh make ova          # or any target from buildroot-external/configs
```

Images land in `output/images/` and are named `asyncos_<board>-<version>.<ext>`.

Run `scripts/enter.sh make help` to list every target.

## Components

| Layer          | Choice                                                                                  |
| -------------- | --------------------------------------------------------------------------------------- |
| Bootloader     | [GRUB](https://www.gnu.org/software/grub/) on UEFI, [U-Boot](https://u-boot.org/) elsewhere |
| Base system    | [Buildroot](https://buildroot.org/) LTS, systemd                                        |
| File systems   | EROFS (read-only root), ext4 (data), ZRAM for `/tmp`, `/var` and swap                   |
| Containers     | [Docker Engine](https://docs.docker.com/engine/)                                        |
| Updates        | [RAUC](https://rauc.io/) A/B slots, OTA and USB                                          |
| Security       | [AppArmor](https://apparmor.net/)                                                        |

## Brand

Logos and color tokens live in [`branding/`](branding/). The product name and IDs used at build time are set once in [`buildroot-external/meta`](buildroot-external/meta).

## Upstream and license

AsyncOS is derived from [Home Assistant Operating System](https://github.com/home-assistant/operating-system) and is distributed under the [Apache License 2.0](LICENSE). AsyncOS is an independent project and is not affiliated with or endorsed by Home Assistant or the Open Home Foundation.
