package com.zflip5.modem.ui.theme

import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class ThemeTest {
    @Test fun darkSchemeUsesDesignTokens() {
        assertEquals(Color(0xFFA9CDFB), FlippostDarkScheme.primary)
        assertEquals(Color(0xFF1B2436), FlippostDarkScheme.onPrimary)
        assertEquals(Color(0xFF1B2436), FlippostDarkScheme.background)
        assertEquals(Color(0xFFFEF7EE), FlippostDarkScheme.onSurface)
        assertEquals(Color(0xFF9EB1CB), FlippostDarkScheme.onSurfaceVariant)
        assertEquals(Color(0xFFFF9AA6), FlippostDarkScheme.error)
        assertEquals(Color(0xFF9EE6C3), FlippostColors.green)
        assertEquals(Color(0xFFFFD49A), FlippostColors.amber)
        assertEquals(Color(0x12D5E7FD), FlippostColors.glass)
        assertEquals(Color(0x26D5E7FD), FlippostColors.line)
    }
}
