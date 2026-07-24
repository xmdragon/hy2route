include $(TOPDIR)/rules.mk

PKG_NAME:=hy2route
PKG_VERSION:=0.2.0
PKG_RELEASE:=1
PKG_LICENSE:=MIT

include $(INCLUDE_DIR)/package.mk

define Package/hy2route
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Minimal hybrid relay transparent proxy
  DEPENDS:=+nftables-json +kmod-nft-tproxy +ip-full +ucode +ucode-mod-uci +dnsmasq-full +ca-bundle +luci-base
endef

define Package/hy2route/description
 A small OpenWrt service for VLESS/HY2 relay to SOCKS/HTTP landing chains,
 with direct HY2 UDP egress, mainland China bypass and explicit overrides.
endef

define Package/hy2route/conffiles
/etc/config/hy2route
endef

define Package/hy2route/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
chown root:root /etc/config/hy2route /etc/init.d/hy2route \
	/usr/bin/hy2route /usr/libexec/hy2route/generate.uc \
	/etc/sysctl.d/90-hy2route.conf \
	/usr/share/hy2route/china4.nft \
	/usr/share/luci/menu.d/luci-app-hy2route.json \
	/usr/share/rpcd/acl.d/luci-app-hy2route.json \
	/www/luci-static/resources/view/hy2route/main.js
chmod 600 /etc/config/hy2route
chmod 755 /etc/init.d/hy2route /usr/bin/hy2route /usr/libexec/hy2route/generate.uc
chmod 644 /usr/share/hy2route/china4.nft
chmod 644 /etc/sysctl.d/90-hy2route.conf
chmod 644 /usr/share/luci/menu.d/luci-app-hy2route.json \
	/usr/share/rpcd/acl.d/luci-app-hy2route.json \
	/www/luci-static/resources/view/hy2route/main.js
sysctl -p /etc/sysctl.d/90-hy2route.conf >/dev/null 2>&1 || true
rm -f /tmp/luci-indexcache
endef

define Build/Compile
	$(CURDIR)/tools/build-core.sh
endef

define Package/hy2route/install
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/etc/config/hy2route $(1)/etc/config/hy2route
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/etc/init.d/hy2route $(1)/etc/init.d/hy2route
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) ./build/hy2route-core $(1)/usr/bin/hy2route-core
	$(INSTALL_BIN) ./files/usr/bin/hy2route $(1)/usr/bin/hy2route
	$(INSTALL_DIR) $(1)/etc/sysctl.d
	$(INSTALL_DATA) ./files/etc/sysctl.d/90-hy2route.conf $(1)/etc/sysctl.d/90-hy2route.conf
	$(INSTALL_DIR) $(1)/usr/libexec/hy2route
	$(INSTALL_BIN) ./files/usr/libexec/hy2route/generate.uc $(1)/usr/libexec/hy2route/generate.uc
	$(INSTALL_DIR) $(1)/usr/share/hy2route
	$(INSTALL_DATA) ./build/hy2route-data.bin $(1)/usr/share/hy2route/routing.bin
	$(INSTALL_DATA) ./files/usr/share/hy2route/china4.nft $(1)/usr/share/hy2route/china4.nft
	$(INSTALL_DIR) $(1)/usr/share/luci/menu.d
	$(INSTALL_DATA) ./files/usr/share/luci/menu.d/luci-app-hy2route.json $(1)/usr/share/luci/menu.d/luci-app-hy2route.json
	$(INSTALL_DIR) $(1)/usr/share/rpcd/acl.d
	$(INSTALL_DATA) ./files/usr/share/rpcd/acl.d/luci-app-hy2route.json $(1)/usr/share/rpcd/acl.d/luci-app-hy2route.json
	$(INSTALL_DIR) $(1)/www/luci-static/resources/view/hy2route
	$(INSTALL_DATA) ./files/www/luci-static/resources/view/hy2route/main.js $(1)/www/luci-static/resources/view/hy2route/main.js
endef

$(eval $(call BuildPackage,hy2route))
