package com.zflip5.modem

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import com.zflip5.modem.ui.theme.FlippostTheme
import com.zflip5.modem.ui.theme.drizzle

class CoverKioskActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent { FlippostTheme { Box(Modifier.fillMaxSize().drizzle()) } }
    }
}
