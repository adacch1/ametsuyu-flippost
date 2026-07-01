#!/bin/bash
# Test stub mimicking `adb` for discover-device + collect-capabilities tests.
# Called as: fake-adb.sh [-s SERIAL] <command...>. Dispatch on the full arg
# string; unknown commands echo nothing. Order: most specific first.
set -u

if [ "${1:-}" = "-s" ]; then shift 2; fi
args="$*"
case "$args" in
  "get-state")
    echo "device" ;;
  "shell getprop"*)
    cat <<'EOF'
[ro.product.model]: [SM-F731U]
[ro.product.manufacturer]: [samsung]
[ro.build.version.release]: [14]
[ro.build.version.sdk]: [34]
[ro.serialno]: [R5CWA1B2C3D]
[ro.boot.serialno]: [R5CWA1B2C3D]
[ro.boot.warranty_bit]: [1]
[ro.boot.flash.locked]: [0]
[ro.config.knox]: [v30]
EOF
    ;;
  *magisk*)
    echo "27000" ;;
  *telephony.registry*)
    cat <<'EOF'
mServiceState=1 mDataNetworkType=13(LTE)
mImei=351756051523999 mImsi=310170123456789
mSubId=1 mActiveSubscriptionInfoList carrier=Example
EOF
    ;;
  *"cmd phone"*)
    printf 'usage: cmd phone [subcommand]\n  set-allowed-network-types-for-reason\n  get-allowed-network-types\n' ;;
  *"cmd wifi"*)
    printf 'usage: cmd wifi [subcommand]\n  start-softap\n  stop-softap\n' ;;
  *"cmd connectivity"*)
    printf 'usage: cmd connectivity [subcommand]\n  tethering\n' ;;
  *connectivity*)
    echo "NetworkAgentInfo network{100} type: MOBILE state: CONNECTED" ;;
  *battery*)
    printf '  level: 87\n  temperature: 412\n  status: 2\n' ;;
  *thermal_zone*)
    printf 'cpu-0-0=41400\ncpu-1-0=43600\ngpuss-0=42600\nxo-therm=39796\nbattery=33100\n' ;;
  *thermal*)
    printf 'battery\ncpu-0-0-usr\nskin-therm\n' ;;
  *isub*|*"SubscriptionManager"*)
    echo "{id=1 iccId=8901260123456789012 carrier=Example}" ;;
  *"list permissions"*)
    printf 'permission:android.permission.READ_SMS\npermission:android.permission.RECEIVE_SMS\n' ;;
  *appops*)
    printf 'usage: appops [set|get|reset]\n' ;;
  *"settings list"*)
    printf 'mobile_data=1\ntether_dun_required=0\npreferred_network_mode1=27\n' ;;
  *svc*)
    printf 'svc usb|wifi|data|power|nfc|bluetooth\n' ;;
  *"proc/net/tcp"*|*" ss "*|*"netstat"*)
    echo "tcp  0  0 127.0.0.1:18080  0.0.0.0:*  LISTEN" ;;
  *"proc/meminfo"*)
    printf 'MemTotal:        7329468 kB\nMemFree:          658968 kB\nMemAvailable:    4058836 kB\n' ;;
  *"proc/loadavg"*)
    echo "0.90 0.84 0.68 1/4816 22716" ;;
  *"proc/cpuinfo"*)
    printf 'processor\t: 0\nprocessor\t: 1\nprocessor\t: 2\nprocessor\t: 3\nprocessor\t: 4\nprocessor\t: 5\nprocessor\t: 6\nprocessor\t: 7\n' ;;
  *)
    : ;;
esac
