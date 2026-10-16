################################################################################
#
# asyncos-kiosk
#
################################################################################

ASYNCOS_KIOSK_VERSION = 1.0
ASYNCOS_KIOSK_SITE = $(BR2_EXTERNAL_ASYNCOS_PATH)/package/asyncos-kiosk
ASYNCOS_KIOSK_SITE_METHOD = local
ASYNCOS_KIOSK_LICENSE = Apache-2.0
ASYNCOS_KIOSK_DEPENDENCIES = asyncos-controller cog

define ASYNCOS_KIOSK_USERS
	kiosk -1 kiosk -1 * - - video,input,render AsyncOS kiosk display
endef

define ASYNCOS_KIOSK_INSTALL_INIT_SYSTEMD
	$(INSTALL) -D -m 0644 $(ASYNCOS_KIOSK_PKGDIR)/asyncos-kiosk.service \
		$(TARGET_DIR)/usr/lib/systemd/system/asyncos-kiosk.service
	$(INSTALL) -D -m 0644 $(ASYNCOS_KIOSK_PKGDIR)/local-display.conf \
		$(TARGET_DIR)/usr/lib/systemd/system/asyncos-controller.service.d/local-display.conf
endef

$(eval $(generic-package))
