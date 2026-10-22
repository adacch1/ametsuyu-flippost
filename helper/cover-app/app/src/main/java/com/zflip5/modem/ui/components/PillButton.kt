package com.zflip5.modem.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.zflip5.modem.ui.theme.FlippostColors

@Composable
fun PillButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    primary: Boolean = false,
    destructive: Boolean = false,
    compact: Boolean = false,
) {
    val shape = RoundedCornerShape(22.dp)
    val face = when {
        primary -> Modifier.background(Brush.horizontalGradient(listOf(Color(0xFFA9CDFB), Color(0xFF6FA4F0))), shape)
        else -> Modifier.background(FlippostColors.glass, shape)
            .border(1.dp, if (destructive) FlippostColors.red else FlippostColors.line, shape)
    }
    val textColor = when {
        primary -> MaterialTheme.colorScheme.onPrimary
        destructive -> FlippostColors.red
        else -> MaterialTheme.colorScheme.onSurface
    }
    Box(
        modifier.alpha(if (enabled) 1f else 0.4f).heightIn(min = 48.dp).widthIn(min = 48.dp).then(face)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick).padding(horizontal = if (compact) 8.dp else 16.dp, vertical = if (compact) 8.dp else 0.dp),
        contentAlignment = Alignment.Center,
    ) {
        val style = MaterialTheme.typography.titleMedium
        Text(
            text, color = textColor, style = if (compact) style.copy(fontSize = 12.5.sp) else style,
            textAlign = TextAlign.Center, maxLines = 1, softWrap = false,
        )
    }
}
