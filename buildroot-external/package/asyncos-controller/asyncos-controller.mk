################################################################################
#
# asyncos-controller
#
################################################################################

ASYNCOS_CONTROLLER_VERSION = $(shell . $(BR2_EXTERNAL_ASYNCOS_PATH)/meta && echo "$${VERSION_MAJOR}.$${VERSION_MINOR}$${VERSION_SUFFIX:+.$${VERSION_SUFFIX}}")
ASYNCOS_CONTROLLER_SITE = $(BR2_EXTERNAL_ASYNCOS_PATH)/../controller
ASYNCOS_CONTROLLER_SITE_METHOD = local
ASYNCOS_CONTROLLER_LICENSE = Apache-2.0, OFL-1.1 (Inter font)
ASYNCOS_CONTROLLER_LICENSE_FILES = web/static/assets/fonts/LICENSE.txt
ASYNCOS_CONTROLLER_GOMOD = github.com/the-asad-iqbal/async-os/controller
ASYNCOS_CONTROLLER_BUILD_TARGETS = cmd/asyncos-controller
ASYNCOS_CONTROLLER_LDFLAGS = -s -w -X main.version=$(ASYNCOS_CONTROLLER_VERSION)

define ASYNCOS_CONTROLLER_USERS
	asyncos -1 asyncos -1 * - - dialout AsyncOS controller
endef

define ASYNCOS_CONTROLLER_INSTALL_INIT_SYSTEMD
	$(INSTALL) -D -m 0644 $(ASYNCOS_CONTROLLER_PKGDIR)/asyncos-controller.service \
		$(TARGET_DIR)/usr/lib/systemd/system/asyncos-controller.service
	$(INSTALL) -D -m 0644 $(ASYNCOS_CONTROLLER_PKGDIR)/asyncos-reboot.path \
		$(TARGET_DIR)/usr/lib/systemd/system/asyncos-reboot.path
	$(INSTALL) -D -m 0644 $(ASYNCOS_CONTROLLER_PKGDIR)/asyncos-reboot.service \
		$(TARGET_DIR)/usr/lib/systemd/system/asyncos-reboot.service
	$(INSTALL) -D -m 0644 $(ASYNCOS_CONTROLLER_PKGDIR)/asyncos.tmpfiles \
		$(TARGET_DIR)/usr/lib/tmpfiles.d/asyncos.conf
endef

$(eval $(golang-package))
