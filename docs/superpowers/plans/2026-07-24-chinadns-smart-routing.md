# ChinaDNS Smart Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate the router's installed ChinaDNS-NG into hy2route so DNS-based CDN selection returns mainland addresses when available and trusted proxied answers otherwise.

**Architecture:** dnsmasq forwards ordinary queries to a procd-managed ChinaDNS-NG listener. ChinaDNS-NG queries `bootstrap_dns` directly and the existing Xray DNS inbound over TCP, then accepts the direct answer only when its A records belong to hy2route's `china4` nftables set. Explicit domain overrides retain their current dnsmasq nftset behavior.

**Tech Stack:** OpenWrt 23.05 rc.common/procd, POSIX shell, ucode/UCI, dnsmasq-full, nftables, ChinaDNS-NG 2025.08.09, LuCI JavaScript.

## Global Constraints

- `smart_dns` defaults to `1`; legacy release 12 DNS behavior remains available only when it is explicitly set to `0`.
- Missing or non-starting ChinaDNS-NG fails hy2route startup and must not silently select another resolver path.
- `smart_dns_port` defaults to `65353` and must differ from `dns_port`, `transparent_port`, and `test_socks_port`.
- The direct upstream is `bootstrap_dns`; the trusted upstream is `tcp://127.0.0.1#<dns_port>`.
- ChinaDNS-NG tests A answers against `inet@hy2route@china4`, filters AAAA, and uses a correctly typed empty `inet@hy2route@china6` set for its IPv6 set argument.
- Explicit proxy rules continue to win over explicit direct rules and `china4`.
- Do not replace the live `/etc/config/hy2route` during deployment.
- Do not vendor an architecture-specific ChinaDNS-NG binary or add an unresolved OpenWrt package dependency.
- Package release becomes `13`.

---

### Task 1: Generate and expose smart DNS configuration

**Files:**
- Create: `tests/test_smart_dns_generator_contract.sh`
- Modify: `files/etc/config/hy2route`
- Modify: `files/usr/libexec/hy2route/generate.uc`
- Modify: `files/www/luci-static/resources/view/hy2route/main.js`

**Interfaces:**
- Consumes: existing UCI `main` options, generator modes, `china4`, dnsmasq explicit domain rules.
- Produces: UCI options `smart_dns` and `smart_dns_port`, generator mode `chinadns`, nft set `china6`, and smart dnsmasq upstream selection.

- [ ] **Step 1: Write the failing generator contract**

Create `tests/test_smart_dns_generator_contract.sh`:

```sh
#!/bin/sh
set -eu

generator="files/usr/libexec/hy2route/generate.uc"
config="files/etc/config/hy2route"
luci="files/www/luci-static/resources/view/hy2route/main.js"

require_literal() {
	file="$1"
	literal="$2"
	if ! grep -Fq "$literal" "$file"; then
		echo "missing smart DNS contract in $file: $literal" >&2
		exit 1
	fi
}

require_block() {
	file="$1"
	block="$2"
	normalized_file="$(tr '\n' '\034' < "$file")"
	normalized_block="$(printf '%s' "$block" | tr '\n' '\034')"
	if ! printf '%s' "$normalized_file" | grep -Fq "$normalized_block"; then
		echo "missing smart DNS block in $file: $block" >&2
		exit 1
	fi
}

require_literal "$config" "option smart_dns '1'"
require_literal "$config" "option smart_dns_port '65353'"

require_literal "$generator" "const smart_dns = boolean(main.smart_dns, true);"
require_literal "$generator" "const smart_dns_port = number(main.smart_dns_port, 65353, 1, 65535, 'smart_dns_port');"
require_literal "$generator" "fail('smart_dns_port must differ from transparent_port, test_socks_port and dns_port');"
require_literal "$generator" "print('\\tset china6 {\\n\\t\\ttype ipv6_addr\\n\\t\\tflags interval\\n\\t\\tauto-merge\\n\\t}\\n');"
require_literal "$generator" "function emit_chinadns() {"
require_literal "$generator" "print('bind-addr 127.0.0.1\\n');"
require_literal "$generator" "print('bind-port ' + smart_dns_port + '\\n');"
require_literal "$generator" "print('china-dns ' + bootstrap_dns + '\\n');"
require_literal "$generator" "print('trust-dns tcp://127.0.0.1#' + dns_port + '\\n');"
require_literal "$generator" "print('ipset-name4 inet@hy2route@china4\\n');"
require_literal "$generator" "print('ipset-name6 inet@hy2route@china6\\n');"
require_literal "$generator" "print('no-ipv6\\n');"
require_literal "$generator" "print('cache 1024\\n');"
require_literal "$generator" "print('verdict-cache 1024\\n');"
require_block "$generator" "if (smart_dns)
		print('server=127.0.0.1#' + smart_dns_port + '\\n');
	else if (tcp_relay_enabled || landing_type == 'socks')
		print('server=127.0.0.1#' + dns_port + '\\n');
	else
		print('server=' + bootstrap_dns + '\\n');"
require_literal "$generator" "else if (mode == 'chinadns')"
require_literal "$generator" "fail(\"usage: generate.uc <xray|nft|dnsmasq|chinadns>\");"

require_literal "$luci" "o = s.taboption('general', form.Flag, 'smart_dns', _('智能 DNS'));"
require_literal "$luci" "o.default = o.enabled;"
require_literal "$luci" "['smart_dns_port', _('智能 DNS 端口'), '65353']"

echo 'smart DNS generator contract tests passed'
```

- [ ] **Step 2: Run the contract and verify RED**

Run:

```sh
chmod 755 tests/test_smart_dns_generator_contract.sh
tests/test_smart_dns_generator_contract.sh
```

Expected: exit `1`, first failure reports missing `option smart_dns '1'`.

- [ ] **Step 3: Add UCI and LuCI options**

Add after `dns_port` in `files/etc/config/hy2route`:

```text
	option smart_dns '1'
	option smart_dns_port '65353'
```

Add after `remote_dns` in the LuCI general tab:

```javascript
		o = s.taboption('general', form.Flag, 'smart_dns', _('智能 DNS'),
			_('同时查询国内 DNS 和经代理访问的远程 DNS；国内答案属于大陆 IP 时优先采用。'));
		o.default = o.enabled;
		o.rmempty = false;
```

Add this entry to the existing advanced port array:

```javascript
		['smart_dns_port', _('智能 DNS 端口'), '65353']
```

- [ ] **Step 4: Parse and validate the generator settings**

Add beside the existing port constants:

```javascript
const smart_dns = boolean(main.smart_dns, true);
const smart_dns_port = number(main.smart_dns_port, 65353, 1, 65535, 'smart_dns_port');
```

Add after IPv4 resolver validation:

```javascript
if (smart_dns && (smart_dns_port == transparent_port ||
	smart_dns_port == test_socks_port || smart_dns_port == dns_port))
	fail('smart_dns_port must differ from transparent_port, test_socks_port and dns_port');
```

- [ ] **Step 5: Generate ChinaDNS and nftables configuration**

Immediately after the generated `china4` set, emit:

```javascript
	print('\tset china6 {\n\t\ttype ipv6_addr\n\t\tflags interval\n\t\tauto-merge\n\t}\n');
```

Add before `emit_dnsmasq()`:

```javascript
function emit_chinadns() {
	print('bind-addr 127.0.0.1\n');
	print('bind-port ' + smart_dns_port + '\n');
	print('china-dns ' + bootstrap_dns + '\n');
	print('trust-dns tcp://127.0.0.1#' + dns_port + '\n');
	print('ipset-name4 inet@hy2route@china4\n');
	print('ipset-name6 inet@hy2route@china6\n');
	print('no-ipv6\n');
	print('cache 1024\n');
	print('verdict-cache 1024\n');
}
```

Replace the final upstream branch in `emit_dnsmasq()` with:

```javascript
	if (smart_dns)
		print('server=127.0.0.1#' + smart_dns_port + '\n');
	else if (tcp_relay_enabled || landing_type == 'socks')
		print('server=127.0.0.1#' + dns_port + '\n');
	else
		print('server=' + bootstrap_dns + '\n');
```

Add the generator dispatch:

```javascript
else if (mode == 'chinadns')
	emit_chinadns();
```

Change the usage failure to:

```javascript
fail("usage: generate.uc <xray|nft|dnsmasq|chinadns>");
```

- [ ] **Step 6: Run the contract and verify GREEN**

Run:

```sh
tests/test_smart_dns_generator_contract.sh
tests/test_udp_route_contract.sh
tests/test_tcp_relay_contract.sh
```

Expected: all three scripts exit `0` and print their respective passed messages.

- [ ] **Step 7: Commit Task 1**

```sh
git add tests/test_smart_dns_generator_contract.sh \
	files/etc/config/hy2route \
	files/usr/libexec/hy2route/generate.uc \
	files/www/luci-static/resources/view/hy2route/main.js
git commit -m "feat: generate chinadns smart routing"
```

---

### Task 2: Manage ChinaDNS lifecycle and fail closed

**Files:**
- Create: `tests/test_smart_dns_lifecycle_contract.sh`
- Modify: `files/etc/init.d/hy2route`
- Modify: `files/usr/bin/hy2route`

**Interfaces:**
- Consumes: generated `/tmp/hy2route/chinadns.conf`, UCI `smart_dns`, nft sets installed by the init script.
- Produces: a `chinadns` procd instance, startup probing and rollback, smart-aware `check` and `status`.

- [ ] **Step 1: Write the failing lifecycle contract**

Create `tests/test_smart_dns_lifecycle_contract.sh`:

```sh
#!/bin/sh
set -eu

init="files/etc/init.d/hy2route"
cli="files/usr/bin/hy2route"

require_literal() {
	file="$1"
	literal="$2"
	if ! grep -Fq "$literal" "$file"; then
		echo "missing smart DNS lifecycle contract in $file: $literal" >&2
		exit 1
	fi
}

require_block() {
	file="$1"
	block="$2"
	normalized_file="$(tr '\n' '\034' < "$file")"
	normalized_block="$(printf '%s' "$block" | tr '\n' '\034')"
	if ! printf '%s' "$normalized_file" | grep -Fq "$normalized_block"; then
		echo "missing smart DNS lifecycle block in $file: $block" >&2
		exit 1
	fi
}

require_literal "$init" 'CHINADNS=/usr/bin/chinadns-ng'
require_literal "$init" 'smart_dns_enabled() {'
require_literal "$init" '"$GEN" chinadns > "$RUNDIR/chinadns.conf" || return 1'
require_literal "$init" '[ -x "$CHINADNS" ] || return 1'
require_literal "$init" 'probe_chinadns() {'
require_literal "$init" '"$CHINADNS" -C "$RUNDIR/chinadns.conf" > "$RUNDIR/chinadns.probe.log" 2>&1 &'
require_literal "$init" 'logger -t hy2route "ChinaDNS startup probe failed: $probe_error"'
require_block "$init" 'if smart_dns_enabled && ! probe_chinadns; then
		remove_network_rules
		return 1
	fi'
require_literal "$init" 'procd_open_instance chinadns'
require_literal "$init" 'procd_set_param command "$CHINADNS" -C "$RUNDIR/chinadns.conf"'
require_literal "$init" 'procd_set_param respawn 3600 5 5'

require_literal "$cli" 'CHINADNS=/usr/bin/chinadns-ng'
require_literal "$cli" '"$GEN" chinadns > "$RUNDIR/chinadns.check.conf" || exit 1'
require_literal "$cli" '[ -x "$CHINADNS" ] || {'
require_literal "$cli" "ps w | grep -q '[c]hinadns-ng -C /tmp/hy2route/chinadns.conf'"

echo 'smart DNS lifecycle contract tests passed'
```

- [ ] **Step 2: Run the contract and verify RED**

Run:

```sh
chmod 755 tests/test_smart_dns_lifecycle_contract.sh
tests/test_smart_dns_lifecycle_contract.sh
```

Expected: exit `1` with missing `CHINADNS=/usr/bin/chinadns-ng`.

- [ ] **Step 3: Add smart mode and startup probe helpers**

Add to `files/etc/init.d/hy2route` constants:

```sh
CHINADNS=/usr/bin/chinadns-ng
```

Add after `config_value()`:

```sh
smart_dns_enabled() {
	local value
	value="$(config_value smart_dns)"
	[ "${value:-1}" = '1' ]
}
```

Add after `restore_dnsmasq()`:

```sh
probe_chinadns() {
	local pid probe_error
	"$CHINADNS" -C "$RUNDIR/chinadns.conf" > "$RUNDIR/chinadns.probe.log" 2>&1 &
	pid=$!
	sleep 1
	if ! kill -0 "$pid" >/dev/null 2>&1; then
		wait "$pid" 2>/dev/null || true
		probe_error="$(tail -n 1 "$RUNDIR/chinadns.probe.log" 2>/dev/null)"
		logger -t hy2route "ChinaDNS startup probe failed: $probe_error"
		return 1
	fi
	kill "$pid" >/dev/null 2>&1 || true
	wait "$pid" 2>/dev/null || true
	rm -f "$RUNDIR/chinadns.probe.log"
}
```

- [ ] **Step 4: Generate, validate, and roll back ChinaDNS startup**

Extend `prepare_config()` after dnsmasq generation:

```sh
	if smart_dns_enabled; then
		[ -x "$CHINADNS" ] || return 1
		"$GEN" chinadns > "$RUNDIR/chinadns.conf" || return 1
	else
		rm -f "$RUNDIR/chinadns.conf"
	fi
```

After policy route installation and before copying the dnsmasq snippet, add:

```sh
	if smart_dns_enabled && ! probe_chinadns; then
		remove_network_rules
		return 1
	fi
```

This ordering is required because ChinaDNS validates the nft set handles at
startup and the live `hy2route` table must already exist.

- [ ] **Step 5: Register the named procd instance**

Name the existing Xray instance:

```sh
	procd_open_instance xray
```

After closing the Xray instance, add:

```sh
	if smart_dns_enabled; then
		procd_open_instance chinadns
		procd_set_param command "$CHINADNS" -C "$RUNDIR/chinadns.conf"
		procd_set_param respawn 3600 5 5
		procd_set_param limits nofile='4096 4096'
		procd_set_param stdout 0
		procd_set_param stderr 1
		procd_close_instance
	fi
```

- [ ] **Step 6: Make CLI checks and status smart-aware**

Add to `files/usr/bin/hy2route`:

```sh
CHINADNS=/usr/bin/chinadns-ng
```

In the `check` case, after generating nft configuration:

```sh
		smart_dns="$(uci -q get hy2route.main.smart_dns)"
		[ -n "$smart_dns" ] || smart_dns=1
		if [ "$smart_dns" = '1' ]; then
			[ -x "$CHINADNS" ] || {
				echo "missing executable: $CHINADNS" >&2
				exit 1
			}
			"$GEN" chinadns > "$RUNDIR/chinadns.check.conf" || exit 1
			"$CHINADNS" --version >/dev/null || exit 1
		fi
```

Replace the `status` branch with:

```sh
		smart_dns="$(uci -q get hy2route.main.smart_dns)"
		[ -n "$smart_dns" ] || smart_dns=1
		if nft list table inet hy2route >/dev/null 2>&1 &&
			ps w | grep -q '[x]ray run -c /tmp/hy2route/xray.json' &&
			{ [ "$smart_dns" != '1' ] ||
				ps w | grep -q '[c]hinadns-ng -C /tmp/hy2route/chinadns.conf'; }; then
			echo 'hy2route is running'
		else
			echo 'hy2route is stopped'
			exit 1
		fi
```

- [ ] **Step 7: Run lifecycle and regression contracts**

Run:

```sh
tests/test_smart_dns_lifecycle_contract.sh
tests/test_smart_dns_generator_contract.sh
tests/test_package_contract.sh
tests/test_udp_route_contract.sh
tests/test_tcp_relay_contract.sh
```

Expected: all scripts exit `0`.

- [ ] **Step 8: Commit Task 2**

```sh
git add tests/test_smart_dns_lifecycle_contract.sh \
	files/etc/init.d/hy2route \
	files/usr/bin/hy2route
git commit -m "feat: supervise chinadns with fail-closed startup"
```

---

### Task 3: Document and release smart DNS

**Files:**
- Modify: `README.md`
- Modify: `Makefile`
- Modify: `tests/test_package_contract.sh`

**Interfaces:**
- Consumes: implemented UCI, generator, and lifecycle behavior.
- Produces: release 13 package contract and operator documentation.

- [ ] **Step 1: Make the package test fail on the new release contract**

In `tests/test_package_contract.sh`, replace:

```sh
require_literal Makefile 'PKG_RELEASE:=12'
```

with:

```sh
require_literal Makefile 'PKG_RELEASE:=13'
require_literal files/etc/config/hy2route "option smart_dns '1'"
require_literal files/etc/config/hy2route "option smart_dns_port '65353'"
```

Run:

```sh
tests/test_package_contract.sh
```

Expected: exit `1`, reporting missing `PKG_RELEASE:=13`.

- [ ] **Step 2: Increment the package release**

Change in `Makefile`:

```make
PKG_RELEASE:=13
```

Do not add `chinadns-ng` to `DEPENDS`; the stock SDK workflow does not import
the third-party recipe.

- [ ] **Step 3: Update the README**

Add to the topology and DNS design sections:

```text
DNS (smart mode): dnsmasq -> ChinaDNS-NG
  mainland candidate -> configured bootstrap DNS directly
  trusted candidate  -> Xray DNS inbound -> remote DNS through relay
```

Document these operator facts:

- smart mode is enabled by default and requires `/usr/bin/chinadns-ng`;
- unclassified domains use dual resolution and `china4` answer validation;
- explicit direct and proxy domain rules keep their existing precedence;
- `smart_dns=0` restores release 12 behavior;
- smart mode filters AAAA because this release routes forwarded clients by
  IPv4 only;
- startup fails rather than silently falling back if ChinaDNS is unavailable.

- [ ] **Step 4: Run package and complete local test suite**

Run:

```sh
for test_script in tests/*.sh; do
	"$test_script"
done
git diff --check
```

Expected: every test prints a passed message, the supervisor scenarios
complete without failure, and `git diff --check` exits `0`.

- [ ] **Step 5: Commit Task 3**

```sh
git add README.md Makefile tests/test_package_contract.sh
git commit -m "docs: release chinadns smart routing"
```

---

### Task 4: Review and verify the completed implementation

**Files:**
- Inspect: all files changed since commit `559aa07`

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces: reviewed, locally verified release 13 source ready for the router.

- [ ] **Step 1: Review the complete diff against the specification**

Run:

```sh
git diff 559aa07..HEAD -- \
	Makefile README.md files tests
```

Confirm each global constraint has a corresponding code path or test. Reject
unrelated refactors and any automatic DNS fallback.

- [ ] **Step 2: Re-run fresh verification**

Run:

```sh
set -eu
for test_script in tests/*.sh; do
	"$test_script"
done
git diff --check 559aa07..HEAD
git status --short
```

Expected: all tests exit `0`, no whitespace errors, and the worktree is clean.

---

### Task 5: Back up, deploy, and validate the live router

**Files:**
- Deploy: `files/usr/libexec/hy2route/generate.uc` -> `/usr/libexec/hy2route/generate.uc`
- Deploy: `files/etc/init.d/hy2route` -> `/etc/init.d/hy2route`
- Deploy: `files/usr/bin/hy2route` -> `/usr/bin/hy2route`
- Deploy: `files/www/luci-static/resources/view/hy2route/main.js` -> `/www/luci-static/resources/view/hy2route/main.js`
- Preserve: `/etc/config/hy2route`

**Interfaces:**
- Consumes: verified local release 13 source and installed router binary `/usr/bin/chinadns-ng`.
- Produces: live smart DNS routing with a timestamped root-only rollback backup.

- [ ] **Step 1: Capture pre-deployment state**

On the router:

```sh
stamp="$(date +%Y%m%d-%H%M%S)"
backup="/root/hy2route-backup-$stamp-chinadns"
mkdir -m 700 "$backup"
cp -a /etc/config/hy2route "$backup/config"
cp -a /etc/init.d/hy2route "$backup/init"
cp -a /usr/bin/hy2route "$backup/cli"
cp -a /usr/libexec/hy2route/generate.uc "$backup/generator"
cp -a /www/luci-static/resources/view/hy2route/main.js "$backup/luci"
[ ! -d /tmp/hy2route ] || cp -a /tmp/hy2route "$backup/runtime"
printf '%s\n' "$backup" > /tmp/hy2route-backup-path
printf '%s\n' "$backup"
```

Also record:

```sh
hy2route status
free -m
sha256sum /etc/init.d/hy2route /usr/bin/hy2route \
	/usr/libexec/hy2route/generate.uc \
	/www/luci-static/resources/view/hy2route/main.js
```

- [ ] **Step 2: Stage and install code without replacing UCI**

Copy the four deployment files to `/tmp/hy2route-deploy/`, then on the router:

```sh
cp /tmp/hy2route-deploy/generate.uc /usr/libexec/hy2route/generate.uc
cp /tmp/hy2route-deploy/hy2route.init /etc/init.d/hy2route
cp /tmp/hy2route-deploy/hy2route.cli /usr/bin/hy2route
cp /tmp/hy2route-deploy/main.js /www/luci-static/resources/view/hy2route/main.js
chown root:root /usr/libexec/hy2route/generate.uc /etc/init.d/hy2route \
	/usr/bin/hy2route /www/luci-static/resources/view/hy2route/main.js
chmod 755 /usr/libexec/hy2route/generate.uc /etc/init.d/hy2route \
	/usr/bin/hy2route
chmod 644 /www/luci-static/resources/view/hy2route/main.js
rm -f /tmp/luci-indexcache
uci set hy2route.main.smart_dns='1'
uci set hy2route.main.smart_dns_port='65353'
uci commit hy2route
```

- [ ] **Step 3: Validate before restarting**

On the router:

```sh
test -x /usr/bin/chinadns-ng
chinadns-ng --version
hy2route check
/usr/libexec/hy2route/generate.uc chinadns
/usr/libexec/hy2route/generate.uc dnsmasq
```

Expected: ChinaDNS reports version `2025.08.09`, `hy2route check` reports valid,
the ChinaDNS config uses `192.168.1.1` and `tcp://127.0.0.1#1053`, and dnsmasq
uses `127.0.0.1#65353`.

- [ ] **Step 4: Restart and verify managed state**

On the router:

```sh
/etc/init.d/hy2route restart
hy2route status
ps w | grep -E '[x]ray run -c /tmp/hy2route/xray.json|[c]hinadns-ng -C /tmp/hy2route/chinadns.conf'
nft list set inet hy2route china4 >/dev/null
nft list set inet hy2route china6
ip rule show | grep 'lookup 166'
```

Expected: status is running, both process commands are present, `china6` is an
empty `ipv6_addr` set, and policy rule priority `10066` targets table `166`.

- [ ] **Step 5: Reproduce the original DNS case**

From the workstation:

```sh
dig @192.168.80.1 wechat.com A +short
dig @192.168.80.1 weixin.qq.com A +short
dig @192.168.80.1 www.google.com A +short
```

On the router, verify every IPv4 answer currently returned for the two
WeChat domains:

```sh
for domain in wechat.com weixin.qq.com; do
	for ip in $(nslookup "$domain" 127.0.0.1 |
		awk '/^Address: [0-9]+\./ { print $2 }'); do
		printf '%s %s\n' "$domain" "$ip"
		nft get element inet hy2route china4 "{ $ip }"
	done
done
```

Expected: WeChat queries return mainland addresses accepted by `china4`;
Google returns a valid trusted answer and is not required to belong to
`china4`.

- [ ] **Step 6: Verify end-to-end health and stability**

On the router:

```sh
hy2route test
logread -e hy2route | tail -n 50
free -m
```

Wait through at least one 30-second supervisor sample, then repeat:

```sh
hy2route status
ps w | grep -E '[x]ray run -c /tmp/hy2route/xray.json|[c]hinadns-ng -C /tmp/hy2route/chinadns.conf'
```

Expected: HTTP test returns `204`, no repeated procd crash loop or startup
probe failure appears, and both processes remain present.

- [ ] **Step 7: Roll back immediately if any live check fails**

Load the exact backup path recorded in Step 1:

```sh
backup="$(cat /tmp/hy2route-backup-path)"
test -d "$backup"
/etc/init.d/hy2route stop
cp "$backup/init" /etc/init.d/hy2route
cp "$backup/cli" /usr/bin/hy2route
cp "$backup/generator" /usr/libexec/hy2route/generate.uc
cp "$backup/luci" /www/luci-static/resources/view/hy2route/main.js
cp "$backup/config" /etc/config/hy2route
chown root:root /etc/init.d/hy2route /usr/bin/hy2route \
	/usr/libexec/hy2route/generate.uc \
	/www/luci-static/resources/view/hy2route/main.js \
	/etc/config/hy2route
chmod 755 /etc/init.d/hy2route /usr/bin/hy2route \
	/usr/libexec/hy2route/generate.uc
chmod 644 /www/luci-static/resources/view/hy2route/main.js
chmod 600 /etc/config/hy2route
rm -f /tmp/luci-indexcache
hy2route check
/etc/init.d/hy2route start
hy2route status
```

Do not remove the backup after a successful deployment; report its path for
manual retention or later cleanup.
