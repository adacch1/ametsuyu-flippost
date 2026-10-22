package com.zflip5.modem

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.lifecycle.lifecycleScope
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.SummaryRepository
import com.zflip5.modem.ui.FlippostApp

class CoverKioskActivity : ComponentActivity() {
    private lateinit var repo: SummaryRepository

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        WindowCompat.getInsetsController(window, window.decorView).apply {
            systemBarsBehavior = androidx.core.view.WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            hide(WindowInsetsCompat.Type.systemBars())
        }
        val client = DaemonClient()
        repo = SummaryRepository(client, lifecycleScope)
        setContent { FlippostApp(repo, client) }
    }

    override fun onStart() { super.onStart(); repo.start() }
    override fun onStop() { repo.stop(); super.onStop() }
}
