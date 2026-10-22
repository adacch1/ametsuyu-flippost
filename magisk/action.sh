#!/system/bin/sh
# Magisk Manager Action button. Opens the Flippost app on the cover screen.
am start --user 0 -n com.zflip5.modem/.CoverKioskActivity >/dev/null 2>&1 \
  && echo "Flippost opened" \
  || echo "Could not open Flippost. The module installs the app at boot; reboot once after installing the module."
