<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="branding/logo-inverse.svg">
    <img src="branding/logo.svg" alt="AsyncOS" width="248">
  </picture>
</p>

<p align="center">A dedicated operating system for PV, genset and export-limiting control.</p>

---

AsyncOS turns an industrial PC into a site controller for solar plants with grid and genset connections. It reads energy meters and inverters over Modbus, keeps grid export within the agreed limit, keeps running gensets above their minimum load, and shows everything in a web dashboard on the local network.

## What it does

- **Export limiting.** Holds grid export at or below a set limit, zero export included, with a safety margin. Cuts are immediate; increases are ramped.
- **Genset protection.** Keeps every running genset above its minimum load (percentage or kW), which prevents reverse power and wet stacking. Running state can come from a digital input, debounced for ATS transfers.
- **Fail-safe.** When a required meter stops answering, inverters are curtailed to a safe level or held at their last limit, as the installer chooses.
- **Device library.** Register maps for inverters and meters as data files, plus automatic SunSpec discovery: SMA, Fronius, SolarEdge, KOSTAL, Huawei, Sungrow, Growatt, Eastron, CHINT, Carlo Gavazzi, ABB, Schneider and more. Modbus TCP, RS-485 RTU and RS-485 gateways.
- **Energy dashboard.** Live power flow, day, week, month and year energy, self-consumption and self-sufficiency.
- **Roles and audit.** Viewer, operator, installer and owner roles, an audit log of every change, sessions that end on password or role changes, and login lockout.
- **Kiosk mode.** A full-screen wall display, through revocable display links or on the device's own monitor.
- **Appliance OS.** Read-only root file system, atomic A/B updates with automatic rollback, default-deny firewall, no shell on the local screen.

## Images

| Board ID                | Hardware                                         |
| ----------------------- | ------------------------------------------------ |
| `generic-x86-64`        | UEFI x86-64 industrial PCs                       |
| `generic-x86-64-kiosk`  | The same, with the dashboard on the local screen |
| `ova`                   | Virtual machines (OVA, qcow2, VDI, VHDX, VMDK)   |
| `generic-aarch64`       | UEFI aarch64 systems                             |

Kernel versions are listed in [Documentation/kernel.md](Documentation/kernel.md).

## First start

1. Flash the image and boot. The screen shows the controller's address and a one-time **setup code**.
2. Open `http://asyncos.local` and create the owner account with that code.
3. In **Site setup**, add the Modbus buses and devices, test each connection, and check readings on **Devices**.
4. Set the export limit and genset rules under **Control**, then switch control on.

## Repository layout

| Path                   | Contents                                                       |
| ---------------------- | -------------------------------------------------------------- |
| `controller/`          | The controller service and web UI (Go, no external services)   |
| `buildroot-external/`  | OS configuration, packages, boards and image scripts           |
| `branding/`            | Logo and color tokens                                          |
| `tests/`               | QEMU smoke tests for built images                              |

## Developing the controller

The controller runs on any Linux or macOS machine against a built-in simulated site:

```bash
cd controller
go run ./cmd/asyncos-controller -demo hybrid -listen 127.0.0.1:8080 -data ./tmp
```

Open `http://127.0.0.1:8080` and enter the setup code from `./tmp/setup-code`. The `hybrid` scenario alternates grid operation with genset-backed outages; `grid` is grid-only.

```bash
go test -race ./...
```

## Building images

Builds run in a container, so the only host requirement is Docker.

```bash
git clone --recurse-submodules https://github.com/the-asad-iqbal/async-os.git
cd async-os
scripts/enter.sh make generic_x86_64
```

Images land in `output/images/` as `asyncos_<board>-<version>.<ext>`.

### Update signing

Development builds sign updates with a key generated at build time and trust only that key. Production builds (`DEPLOYMENT="production"` in `buildroot-external/meta`) require the AsyncOS release CA at `buildroot-external/ota/rel-ca.pem`, and CI expects the signing certificate and key in the `RAUC_CERTIFICATE` and `RAUC_PRIVATE_KEY` secrets.

## Upstream and license

AsyncOS is derived from [Home Assistant Operating System](https://github.com/home-assistant/operating-system) and is distributed under the [Apache License 2.0](LICENSE). It is an independent project, not affiliated with or endorsed by Home Assistant or the Open Home Foundation. The Inter typeface is used under the SIL Open Font License.
