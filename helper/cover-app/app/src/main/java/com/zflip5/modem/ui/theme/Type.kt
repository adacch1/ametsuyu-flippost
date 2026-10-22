package com.zflip5.modem.ui.theme

import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import com.zflip5.modem.R

val Nunito = FontFamily(
    listOf(500, 600, 700, 800).map { w ->
        Font(
            R.font.nunito_variable,
            FontWeight(w),
            variationSettings = FontVariation.Settings(FontVariation.weight(w)),
        )
    },
)

private fun style(size: Float, weight: Int, tnum: Boolean = false) = TextStyle(
    fontFamily = Nunito,
    fontSize = size.sp,
    fontWeight = FontWeight(weight),
    fontFeatureSettings = if (tnum) "tnum" else null,
)

val FlippostTypography = Typography(
    displaySmall = style(40f, 700, tnum = true),
    headlineSmall = style(30f, 700, tnum = true),
    titleMedium = style(14.5f, 800),
    titleSmall = style(17f, 800, tnum = true),
    bodyMedium = style(13.5f, 500),
    bodySmall = style(11.5f, 500),
    labelSmall = style(10.5f, 700).copy(letterSpacing = 0.08.em),
)
