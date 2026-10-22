package com.zflip5.modem.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp

private val barHeights = listOf(4.dp, 7.dp, 10.dp, 13.dp)

@Composable
fun SignalBars(bars: Int?, modifier: Modifier = Modifier) {
    val description = if (bars == null) "Signal level unknown" else "Signal $bars of 4 bars"
    Row(
        modifier.semantics { contentDescription = description },
        horizontalArrangement = Arrangement.spacedBy(2.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        barHeights.forEachIndexed { i, h ->
            val reached = bars != null && i < bars
            Box(
                Modifier.width(3.dp).height(h)
                    .background(if (reached) Color(0xFFD5E7FD) else Color(0x40D5E7FD), RoundedCornerShape(1.dp)),
            )
        }
    }
}
