package com.zflip5.modem.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.unit.dp

@Composable
fun FlippostTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = FlippostDarkScheme, typography = FlippostTypography, content = content)
}

private val streaks = listOf(
    13f to 4f to (10f to 13f),
    42f to 17f to (39f to 26f),
    27f to 38f to (24f to 47f),
    56f to 47f to (53f to 56f),
)

/** Sky gradient with faint rain streaks tiled every 64 dp. */
fun Modifier.drizzle(): Modifier = drawBehind {
    drawRect(Brush.verticalGradient(listOf(FlippostColors.skyTop, FlippostColors.skyBottom)))
    val tile = 64.dp.toPx()
    val color = Color(0x17D5E7FD)
    val stroke = 1.3.dp.toPx()
    val dp = 1.dp.toPx()
    var y = 0f
    while (y < size.height) {
        var x = 0f
        while (x < size.width) {
            for ((a, b) in streaks) {
                drawLine(
                    color,
                    Offset(x + a.first * dp, y + a.second * dp),
                    Offset(x + b.first * dp, y + b.second * dp),
                    stroke,
                    StrokeCap.Round,
                )
            }
            x += tile
        }
        y += tile
    }
}
