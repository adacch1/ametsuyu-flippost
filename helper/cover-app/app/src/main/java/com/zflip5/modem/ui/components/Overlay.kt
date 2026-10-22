package com.zflip5.modem.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.zflip5.modem.ui.theme.FlippostColors

/**
 * Full-screen scrim with a card at the top. The card stops above the 31 dp camera cutout
 * (340 dp screen, 22 dp of padding) and scrolls inside itself.
 */
@Composable
fun OverlayCard(onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    // The scrim uses pointerInput, not clickable, so it adds no click target over the cutout.
    Box(Modifier.fillMaxSize().background(Color(0xB3141B29)).pointerInput(onDismiss) { detectTapGestures { onDismiss() } }) {
        val shape = RoundedCornerShape(18.dp)
        Column(
            Modifier.align(Alignment.TopCenter).padding(11.dp).heightIn(max = 340.dp - 31.dp - 22.dp)
                .background(FlippostColors.skyTop, shape).border(1.dp, FlippostColors.line, shape)
                .pointerInput(Unit) { detectTapGestures { } }
                .verticalScroll(rememberScrollState()).padding(11.dp),
            content = content,
        )
    }
}

@Composable
fun ConfirmOverlay(
    title: String,
    body: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    destructive: Boolean = false,
) {
    OverlayCard(onDismiss) {
        Text(title, color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleMedium)
        Text(body, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(top = 4.dp, bottom = 8.dp))
        PillButton(confirmLabel, onConfirm, Modifier.fillMaxWidth().testTag("confirm_button"), primary = !destructive, destructive = destructive)
        Spacer(Modifier.height(6.dp))
        PillButton("Cancel", onDismiss, Modifier.fillMaxWidth().testTag("cancel_button"))
    }
}
