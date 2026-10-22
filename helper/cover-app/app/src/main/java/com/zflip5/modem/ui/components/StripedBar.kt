package com.zflip5.modem.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.TileMode
import androidx.compose.ui.unit.dp
import com.zflip5.modem.ui.theme.FlippostColors

/** Progress bar: 9 dp tall, -45 degree stripes of 6 dp each. */
@Composable
fun StripedBar(fraction: Float, modifier: Modifier = Modifier) {
    val f = fraction.coerceIn(0f, 1f)
    val shape = RoundedCornerShape(50)
    Canvas(modifier.fillMaxWidth().height(9.dp).clip(shape).background(FlippostColors.track)) {
        // The vector (d, -d) is one 12 dp period long, so each color band is 6 dp wide.
        val d = 12.dp.toPx() / 1.41421356f
        val a = Color(0xFFA9CDFB)
        val b = Color(0xFF8DBCF7)
        drawRect(
            Brush.linearGradient(
                0f to a, 0.5f to a, 0.5f to b, 1f to b,
                start = Offset(0f, size.height), end = Offset(d, size.height - d), tileMode = TileMode.Repeated,
            ),
            size = Size(size.width * f, size.height),
        )
    }
}
