#!/system/bin/sh
# firewall.sh — block adb (5555) and the remote-screen ports (8000-8088, 8886)
# on every interface except loopback and tailscale0, for IPv4 and IPv6.
# Usage: sh firewall.sh once   apply the rules once
#        sh firewall.sh        reapply every 60 s in the background, then exit
# Needs no su and no MODDIR, so you can also copy it into /data/adb/service.d.
apply() {
  for T in iptables ip6tables; do
    $T -w -N ZF5_FW 2>/dev/null
    $T -w -F ZF5_FW
    $T -w -A ZF5_FW -i lo -j RETURN
    $T -w -A ZF5_FW -i tailscale0 -j RETURN
    $T -w -A ZF5_FW -p tcp --dport 5555 -j DROP
    $T -w -A ZF5_FW -p tcp --dport 8000:8088 -j DROP
    $T -w -A ZF5_FW -p tcp --dport 8886 -j DROP
    $T -w -C INPUT -j ZF5_FW 2>/dev/null || $T -w -I INPUT 1 -j ZF5_FW
  done
}

if [ "$1" = once ]; then
  apply
else
  ( while :; do apply; sleep 60; done ) &
fi
