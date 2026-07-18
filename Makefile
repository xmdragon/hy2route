include $(TOPDIR)/rules.mk

PKG_NAME:=hy2route
PKG_VERSION:=0.1.0
PKG_RELEASE:=2
PKG_LICENSE:=MIT
PKGARCH:=all

include $(INCLUDE_DIR)/package.mk

define Package/hy2route
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Minimal HY2 chained transparent proxy
  DEPENDS:=+xray-core +nftables-json +kmod-nft-tproxy +ip-full +ucode +ucode-mod-uci +dnsmasq-full +curl +ca-bundle
endef

define Package/hy2route/description
 A small OpenWrt service for HY2 relay to SOCKS/HTTP landing chains,
 with mainland China IP bypass and explicit IP/domain overrides.
endef

define Package/hy2route/conffiles
/etc/config/hy2route
endef

define Package/hy2route/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
chown root:root /etc/config/hy2route /etc/init.d/hy2route \
	/usr/bin/hy2route /usr/libexec/hy2route/generate.uc \
	/usr/share/hy2route/china4.nft
chmod 600 /etc/config/hy2route
chmod 755 /etc/init.d/hy2route /usr/bin/hy2route \
	/usr/libexec/hy2route/generate.uc
chmod 644 /usr/share/hy2route/china4.nft
endef

define Build/Compile
endef

define Package/hy2route/install
	$(INSTALL_DIR) $(1)/etc/config
	$(INSTALL_CONF) ./files/etc/config/hy2route $(1)/etc/config/hy2route
	$(INSTALL_DIR) $(1)/etc/init.d
	$(INSTALL_BIN) ./files/etc/init.d/hy2route $(1)/etc/init.d/hy2route
	$(INSTALL_DIR) $(1)/usr/bin
	$(INSTALL_BIN) ./files/usr/bin/hy2route $(1)/usr/bin/hy2route
	$(INSTALL_DIR) $(1)/usr/libexec/hy2route
	$(INSTALL_BIN) ./files/usr/libexec/hy2route/generate.uc $(1)/usr/libexec/hy2route/generate.uc
	$(INSTALL_DIR) $(1)/usr/share/hy2route
	$(INSTALL_DATA) ./files/usr/share/hy2route/china4.nft $(1)/usr/share/hy2route/china4.nft
endef

$(eval $(call BuildPackage,hy2route))
