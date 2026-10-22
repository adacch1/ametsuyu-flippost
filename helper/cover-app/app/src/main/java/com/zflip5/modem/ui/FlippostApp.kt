package com.zflip5.modem.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.zflip5.modem.data.DaemonClient
import com.zflip5.modem.data.SummaryRepository
import com.zflip5.modem.ui.components.LocalSnackbar
import com.zflip5.modem.ui.home.HomeScreen
import com.zflip5.modem.ui.more.AdvancedScreen
import com.zflip5.modem.ui.more.AlertsScreen
import com.zflip5.modem.ui.more.DataScreen
import com.zflip5.modem.ui.more.DevicesScreen
import com.zflip5.modem.ui.more.HotspotScreen
import com.zflip5.modem.ui.more.MoreScreen
import com.zflip5.modem.ui.more.SystemScreen
import com.zflip5.modem.ui.more.TailscaleScreen
import com.zflip5.modem.ui.theme.FlippostTheme

@Composable
fun FlippostApp(repo: SummaryRepository, client: DaemonClient) {
    val state by repo.state.collectAsState()
    val nav = rememberNavController()
    val snackbar = remember { SnackbarHostState() }
    val back: () -> Unit = { nav.popBackStack() }
    val refresh: () -> Unit = repo::refreshNow
    FlippostTheme {
        CompositionLocalProvider(LocalSnackbar provides snackbar) {
            Box(Modifier.fillMaxSize()) {
                NavHost(nav, startDestination = "home") {
                    composable("home") { HomeScreen(state, client, refresh, onMore = { nav.navigate("more") }) }
                    composable("more") { MoreScreen(onOpen = { nav.navigate(it) }, onBack = back) }
                    composable("devices") { DevicesScreen(state, client, refresh, back) }
                    composable("hotspot") { HotspotScreen(state, client, refresh, back) }
                    composable("data") { DataScreen(state, client, refresh, back) }
                    composable("system") { SystemScreen(state, client, refresh, back) }
                    composable("tailscale") { TailscaleScreen(state, client, refresh, back) }
                    composable("alerts") { AlertsScreen(state, client, refresh, back) }
                    composable("advanced") { AdvancedScreen(state, client, refresh, back) }
                }
                SnackbarHost(snackbar, Modifier.align(Alignment.TopCenter))
            }
        }
    }
}
