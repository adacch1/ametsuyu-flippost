package com.zflip5.modem.ui.components

import org.junit.Assert.assertTrue
import org.junit.Test

class QrCodeTest {
    @Test fun encodesLoginUrl() = assertTrue(qrMatrix("https://login.tailscale.com/a/abc").width >= 21)
}
