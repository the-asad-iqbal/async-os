import logging
from time import sleep

import pytest


_LOGGER = logging.getLogger(__name__)


@pytest.mark.dependency()
@pytest.mark.timeout(300)
def test_init(shell):
    # Wait until systemd finished starting every unit
    while True:
        state = "\n".join(shell.run("systemctl is-system-running")[0])
        if state in ("running", "degraded"):
            break
        sleep(1)

    output = shell.run_check("cat /etc/os-release")
    _LOGGER.info("%s", "\n".join(output))
    assert 'NAME="AsyncOS"' in output


def test_rauc_status(shell):
    rauc_status = shell.run_check("rauc status --output-format=shell --detailed")
    # RAUC_BOOT_PRIMARY won't be set if correct grub env is missing
    assert "RAUC_BOOT_PRIMARY='kernel.0'" in rauc_status
    assert "rauc-WARNING" not in "\n".join(rauc_status)

    expected_version = shell.run_check(". /etc/os-release && echo ${VERSION_ID}")[0]
    assert expected_version != ""

    boot_slots = filter(lambda x: "RAUC_SYSTEM_SLOTS=" in x, rauc_status)
    boot_slots = next(boot_slots, "").replace("RAUC_SYSTEM_SLOTS='", "").replace("'", "")
    assert boot_slots != ""
    booted_idx = boot_slots.split(" ").index("kernel.0")
    assert booted_idx >= 0

    assert f"RAUC_SLOT_STATUS_BUNDLE_VERSION_{booted_idx + 1}='{expected_version}'" in rauc_status


def test_dmesg(shell):
    output = shell.run_check("dmesg")
    _LOGGER.info("%s", "\n".join(output))


def test_systemctl_status(shell):
    output = shell.run_check("systemctl --no-pager -l status -a || true")
    _LOGGER.info("%s", "\n".join(output))


def test_systemctl_check_no_failed(shell):
    output = shell.run_check("systemctl --no-pager -l list-units --state=failed")
    assert "0 loaded units listed." in output, f"Some units failed:\n{"\n".join(output)}"


def test_systemctl_no_cycles(shell):
    # we don't have systemd-analyze available, so check it naively using grep
    output = shell.run_check("journalctl -b0 | grep 'ordering cycle' || true")
    assert not output, f"Found Systemd dependency cycles:\n{"\n".join(output)}"


def test_host_connectivity(shell):
    output = shell.run_check("curl -fsS -o /dev/null -w '%{http_code}' https://www.kernel.org/")
    assert output == ["200"], f"HTTPS request failed: {output}"


@pytest.mark.dependency(depends=["test_init"])
def test_custom_swap_size(shell, target):
    output = shell.run_check("stat -c '%s' /mnt/data/swapfile")
    # set new swap size to half of the previous size - round to 4k blocks
    new_swap_size = (int(output[0]) // 2 // 4096) * 4096
    shell.console.sendline(f"echo 'SWAPSIZE={new_swap_size/1024/1024}M' > /etc/default/haos-swapfile; reboot")
    shell.console.expect("Booting `Slot ", timeout=60)
    # reactivate ShellDriver to handle login again
    target.deactivate(shell)
    target.activate(shell)
    output = shell.run_check("stat -c '%s' /mnt/data/swapfile")
    assert int(output[0]) == new_swap_size, f"Incorrect swap size {new_swap_size}B: {output}"


@pytest.mark.dependency(depends=["test_custom_swap_size"])
def test_no_swap(shell, target):
    shell.console.sendline("echo 'SWAPSIZE=0' > /etc/default/haos-swapfile; reboot")
    shell.console.expect("Booting `Slot ", timeout=60)
    # reactivate ShellDriver to handle login again
    target.deactivate(shell)
    target.activate(shell)
    output = shell.run_check("systemctl --no-pager -l list-units --state=failed")
    assert "0 loaded units listed." in output, f"Some units failed:\n{"\n".join(output)}"
    swapon = shell.run_check("swapon --show")
    assert swapon == [], f"Swapfile still exists: {swapon}"


def test_kernel_not_tainted(shell):
    """Check if the kernel is not tainted - do it at the end of the
    test suite to increase the chance of catching issues."""
    output = shell.run_check("cat /proc/sys/kernel/tainted")
    assert "\n".join(output) == "0", f"Kernel tainted: {output}"


@pytest.mark.dependency(depends=["test_init"])
def test_controller_running(shell):
    output = shell.run_check("systemctl is-active asyncos-controller.service")
    assert output == ["active"], f"Controller not running: {output}"
    health = shell.run_check("curl -fsS http://127.0.0.1/api/v1/health")
    assert '"status":"ok"' in "".join(health), f"Unexpected health response: {health}"


def test_firewall_loaded(shell):
    output = shell.run_check("nft list table inet asyncos")
    assert "policy drop" in "\n".join(output), f"Firewall not loaded: {output}"
