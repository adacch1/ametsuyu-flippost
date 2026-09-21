#!/system/bin/sh
# coverwatch.sh — keep the kiosk on the cover screen.
#
# Samsung's cover policy closes whatever app is on the Flex Window, and this
# firmware has NO generic secondary-home resolution (SystemUI starts
# SubHomeActivity explicitly rather than resolving SECONDARY_HOME), so with the
# stock clock disabled the panel would simply light up empty. This watches the
# panel and puts the kiosk back.
#
# Its own file rather than a block inside service.sh so it can be restarted
# without rebooting the phone.
MODDIR=${0%/*}
DATADIR=/data/adb/zflip5-modem
BL=/sys/class/backlight/panel1-backlight/brightness   # cover panel: ~79 lit, 0 dark (panel0 is the main screen)
n=0

while [ -r "$BL" ]; do
  # Magisk's own switches, and the owner's "give the panel back" switch written
  # by the daemon's /v1/cover/home. Honor the latter or the two fight over the
  # screen every three seconds.
  if [ -f "$MODDIR/disable" ] || [ -f "$MODDIR/remove" ]; then
    break
  fi
  if [ -f "$DATADIR/cover-home-off" ] || [ "$(cat "$BL" 2>/dev/null || echo 0)" = "0" ]; then
    n=0
  else
    # The panel is lit, so the kiosk should be on it. Check that it actually is
    # rather than only reacting to a dark->lit edge: the activity can go away
    # WHILE the panel stays lit (Samsung's cover policy, the owner leaving the
    # app, a component toggle), and waiting for the next edge leaves the cover
    # screen black until then.
    #
    # The tell is the BLAST buffer layer, whose name carries a "$_" suffix and
    # exists only while the window really has a surface on screen. Matching the
    # package alone is WRONG: SurfaceFlinger keeps ActivityRecord entries for a
    # closed activity, so a plain package grep reports a kiosk that is not there
    # and the screen stays black. Reading the layer list is a small dump, so it
    # runs every third second while the brightness read above stays every second.
    n=$((n + 1))
    if [ "$((n % 3))" = "1" ] && ! dumpsys SurfaceFlinger --list 2>/dev/null | grep -q 'CoverKioskActivity\$_'; then
      sh "$MODDIR/action.sh" >/dev/null 2>&1
    fi
  fi
  sleep 1
done
