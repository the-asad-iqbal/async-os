#!/bin/bash
set -e


function prepare_rauc_signing() {
    local key="/build/key.pem"
    local cert="/build/cert.pem"

    if [ ! -f "${key}" ]; then
        echo "Generating a self-signed certificate for development"
        "${BR2_EXTERNAL_ASYNCOS_PATH}"/scripts/generate-signing-key.sh "${cert}" "${key}"
    fi
}


function write_rauc_config() {
    mkdir -p "${TARGET_DIR}/etc/rauc"

    local ota_compatible
    ota_compatible="$(os_rauc_compatible)"

    export ota_compatible
    export BOOTLOADER PARTITION_TABLE_TYPE BOOT_SPL

    (
        "${HOST_DIR}/bin/tempio" \
            -template "${BR2_EXTERNAL_ASYNCOS_PATH}/ota/system.conf.gtpl"
    ) > "${TARGET_DIR}/etc/rauc/system.conf"
}


function install_rauc_certs() {
    local cert="/build/cert.pem"
    local release_ca="${BR2_EXTERNAL_ASYNCOS_PATH}/ota/rel-ca.pem"

    # Production devices only accept bundles signed under the AsyncOS release
    # CA. Every other build trusts the certificate it was signed with.
    if [ "${DEPLOYMENT}" == "production" ]; then
        if [ ! -f "${release_ca}" ]; then
            echo "Production builds need the AsyncOS release CA at ${release_ca}" >&2
            exit 1
        fi
        cp "${release_ca}" "${TARGET_DIR}/etc/rauc/keyring.pem"
    else
        openssl x509 -in "${cert}" -text > "${TARGET_DIR}/etc/rauc/keyring.pem"
    fi
}


function install_bootloader_config() {
    if [ "${BOOTLOADER}" == "uboot" ]; then
        # shellcheck disable=SC1117
        echo -e "/dev/disk/by-partlabel/hassos-bootstate\t0x0000\t${BOOT_ENV_SIZE}" > "${TARGET_DIR}/etc/fw_env.config"
    fi

    # Fix MBR
    if [ "${PARTITION_TABLE_TYPE}" == "mbr" ]; then
        mkdir -p "${TARGET_DIR}/usr/lib/udev/rules.d"
        cp -f "${BR2_EXTERNAL_ASYNCOS_PATH}/bootloader/mbr-part.rules" "${TARGET_DIR}/usr/lib/udev/rules.d/"
    fi
}
