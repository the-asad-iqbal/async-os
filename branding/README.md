# AsyncOS brand

Everything that makes AsyncOS look like AsyncOS lives here. Change a value in this folder or in [`buildroot-external/meta`](../buildroot-external/meta) and it flows into the OS image, the docs and, later, the UI.

## Name

Always **AsyncOS**: one word, capital A, capital OS. Use `asyncos` (lowercase) only where an identifier is needed: hostnames, file names, `os-release` `ID`, RAUC compatible strings.

## Logo

| File                                     | Use                                 |
| ---------------------------------------- | ----------------------------------- |
| [`logo-mark.svg`](logo-mark.svg)         | App icon, favicon, avatars          |
| [`logo.svg`](logo.svg)                   | Wordmark on light backgrounds       |
| [`logo-inverse.svg`](logo-inverse.svg)   | Wordmark on dark backgrounds        |

The mark is an **A** drawn as two strokes that never touch, with a pulse dot between them: independent work that still arrives at one result.

## Colors

The full token set is in [`colors.json`](colors.json). The core palette:

| Token          | Hex       | Role                               |
| -------------- | --------- | ---------------------------------- |
| Indigo 600     | `#5B5CF6` | Primary: actions, links, the "OS"  |
| Indigo 800     | `#3F3BD9` | Mark gradient end, pressed states  |
| Pulse 400      | `#22D3EE` | Accent: the dot, live/active state |
| Ink            | `#0B1020` | Text, dark surfaces                |
| Slate 500      | `#64748B` | Secondary text                     |
| Mist           | `#F5F7FB` | Light surfaces                     |

On terminals the palette maps to xterm-256 color `63` (indigo) and `45` (pulse). The Linux console falls back to its nearest 16-color match.

## Where the brand shows up in the OS

| Surface                    | Source                                                                 |
| -------------------------- | ---------------------------------------------------------------------- |
| `/etc/os-release`          | `OS_*` values in `buildroot-external/meta`, written by `scripts/post-build.sh` |
| systemd boot banner color  | `OS_ANSI_COLOR` in `buildroot-external/meta`                           |
| Login banner (`/etc/issue`) and hostname | `BR2_TARGET_GENERIC_ISSUE` / `BR2_TARGET_GENERIC_HOSTNAME` in `buildroot-external/configs/*_defconfig` |
| Shell welcome (MOTD)       | `buildroot-external/rootfs-overlay/etc/motd`                           |
| GRUB menu                  | `buildroot-external/board/{pc,arm-uefi/generic-aarch64}/grub.cfg`      |
| Virtual appliance          | `buildroot-external/board/pc/ova/asyncos.ovf`                          |
| Image file names           | `OS_ID` → `asyncos_<board>-<version>.<ext>`                             |

## Swapping in final assets

1. Replace the three SVGs, keeping the file names.
2. Update the hex values in `colors.json` and the table above.
3. Update the matching xterm-256 codes in `colors.json` (`terminal`), `OS_ANSI_COLOR` in `buildroot-external/meta` and the escape codes in the MOTD.
