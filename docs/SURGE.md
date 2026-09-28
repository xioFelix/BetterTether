# Using BetterTether beneath Surge Mac

BetterTether already creates a macOS virtual `utunN` interface. It is a
packet-level interface, not a hardware-style `enN` network service. Its number
may change after reconnecting the phone. The desktop app displays the current
`utunN` name as **Virtual adapter** while USB tethering is active; macOS does
not list it among hardware ports in Network Settings.

Surge Enhanced Mode creates a second virtual interface and makes it the
system's default route. BetterTether's normal `0.0.0.0/1` and `128.0.0.0/1`
routes are more specific than that default route, so they can bypass Surge's
VIF. System Proxy alone cannot capture applications that ignore proxy settings.

## Configure BetterTether

Set this in `/etc/bettertether/bettertether.toml` and restart the daemon:

```toml
[route]
set_default_route = true
interface_only = true
```

`interface_only` keeps the phone's `utunN` configured and installs only a
default route scoped to that interface. It does not inject the two `/1` routes or replace the system
DNS configuration. Surge can then own the system default route. This setting
does not change existing installations unless explicitly enabled. The desktop
installer preserves an existing daemon configuration when reinstalling.

## Configure Surge

1. Enable **Enhanced Mode** in Surge Mac. Keep System Proxy on if desired.
2. Find BetterTether's current interface with `ifconfig` or the BetterTether
   connection log. Substitute its `utunN` name below.
3. Bind every desired Surge outbound policy to that interface. For example:

   ```ini
   [Proxy]
   PhoneDirect = direct, interface = utun4, allow-other-interface = false, dns-follow-interface = true
   MyProxy = trojan, example.com, 443, password=example, interface = utun4, allow-other-interface = false, ip-version = v4-only
   ```

4. Select `PhoneDirect` instead of the built-in `DIRECT` in rules and groups
   that must use the phone. For a group that imports a subscription through
   `policy-path`, apply the binding to every imported node with
   `external-policy-modifier="interface=utun4,allow-other-interface=false,ip-version=v4-only"`.
   Surge's WireGuard and Tailscale policies do not support the `interface`
   parameter. An unbound policy may still use Wi-Fi.
5. If raw IPv6 must also be captured, set `ipv6-vif = auto` in Surge's
   `[General]` section. BetterTether's Android RNDIS relay is IPv4-only, so
   direct IPv6 destinations cannot use the phone. Route them through an IPv4
   reachable proxy or expect them to fail.

Check `route -n get 1.1.1.1` after enabling Enhanced Mode: the unscoped route
should point to Surge's VIF, not BetterTether's `utunN`. Check
`route -n get -ifscope utunN 1.1.1.1` to confirm BetterTether still has a
usable scoped route. Finally, inspect Surge's request log and the selected
policy's outgoing interface for representative apps, including apps that do
not use System Proxy.

If the phone reconnects and gets a new `utunN` number, update the Surge
policy bindings. `allow-other-interface = false` deliberately fails closed
rather than silently falling back to Wi-Fi.

### When another VPN uses Surge's fake-IP range

Some VPNs route `198.18.0.0/16`, which can intercept Surge's fake DNS address
(`198.18.0.2`) and the fake addresses it returns. To keep that VPN active,
add these options to a dedicated Surge profile:

```ini
[General]
tun-included-routes = 198.18.0.2/32
always-real-ip = *
encrypted-dns-follow-outbound-mode = true
```

The `/32` route sends only Surge's DNS endpoint to its VIF. Returning real
addresses avoids the other VPN's fake-IP range. This changes Surge's DNS
behavior for every domain, so use a separate profile and verify the rules and
proxy services you rely on. With the phone's IPv4-only RNDIS connection,
`ipv6-vif = auto` can capture raw IPv6 attempts, but direct IPv6 destinations
may fail rather than use the phone.
