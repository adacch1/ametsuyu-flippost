# redact.sh — sourced library. Provides redact(): a stdin->stdout filter that
# strips device identifiers and secrets from discovery output before it is
# written to evidence. Required strips: IMEI, IMSI, ICCID, MSISDN, serial.
#
# Rules (applied per line, in order):
#   1. getprop serial lines  [ro(.boot).serialno]: [VALUE]  -> value masked
#   2. bearer tokens          Bearer <token>                -> token masked
#   3. any digit run >= 10    (IMEI/IMSI/ICCID/MSISDN/phone) -> masked
# Short integers (subId, battery level, ports, raw thermal millideg) survive.
#
# sed -E with these expressions is portable across BSD sed (macOS) and GNU sed.

redact() {
  sed -E \
    -e 's/(\[[a-z0-9._]*serial[a-z0-9._]*\]: \[)[^]]*(\])/\1[REDACTED-SERIAL]\2/' \
    -e 's|(Bearer )[A-Za-z0-9._~+/=-]+|\1[REDACTED-TOKEN]|g' \
    -e 's/[0-9]{10,}/[REDACTED-NUM]/g'
}
