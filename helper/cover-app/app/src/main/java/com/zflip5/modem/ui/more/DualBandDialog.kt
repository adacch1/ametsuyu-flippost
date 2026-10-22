package com.zflip5.modem.ui.more

import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.zflip5.modem.ui.components.OverlayCard
import com.zflip5.modem.ui.components.PillButton

/** Asks when a dual-band change takes effect. Applying now drops clients for about 10 s. */
@Composable
fun DualBandDialog(enable: Boolean, onChoose: (applyNow: Boolean) -> Unit, onDismiss: () -> Unit) {
    OverlayCard(onDismiss) {
        Text(if (enable) "Turn on dual band?" else "Turn off dual band?", color = MaterialTheme.colorScheme.onSurface, style = MaterialTheme.typography.titleMedium)
        Text(
            if (enable) "Dual band serves 2.4 GHz and 5 GHz under one name and stops Wi-Fi scanning." else "The hotspot goes back to 5 GHz only.",
            Modifier.padding(top = 4.dp, bottom = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium,
        )
        PillButton("Apply now (clients drop about 10 s)", { onChoose(true) }, Modifier.fillMaxWidth(), primary = true)
        Spacer(Modifier.height(6.dp))
        PillButton("At next start", { onChoose(false) }, Modifier.fillMaxWidth())
        Spacer(Modifier.height(6.dp))
        PillButton("Cancel", onDismiss, Modifier.fillMaxWidth())
    }
}
