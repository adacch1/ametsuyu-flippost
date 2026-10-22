package com.zflip5.modem

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.junit4.ComposeContentTestRule
import androidx.compose.ui.test.onRoot
import com.zflip5.modem.ui.components.LocalSnackbar
import com.zflip5.modem.ui.theme.FlippostTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue

const val COVER = "w352dp-h340dp-land-xhdpi"

/** Hosts [content] as the cover app does: theme, snackbar host and a full-size root. */
fun ComposeContentTestRule.setCover(content: @Composable () -> Unit) = setContent {
    val snackbar = remember { SnackbarHostState() }
    FlippostTheme {
        CompositionLocalProvider(LocalSnackbar provides snackbar) {
            Box(Modifier.fillMaxSize()) {
                content()
                SnackbarHost(snackbar, Modifier.align(Alignment.TopCenter))
            }
        }
    }
}

private fun ComposeContentTestRule.root() = onRoot().fetchSemanticsNode()

private fun ComposeContentTestRule.cutout(): Rect {
    val r = root()
    val d = r.layoutInfo.density
    val w = r.size.width.toFloat()
    val h = r.size.height.toFloat()
    return Rect(w - 174 * d.density, h - 31 * d.density, w, h)
}

private fun ComposeContentTestRule.clickables() = onAllNodes(hasClickAction(), useUnmergedTree = true).fetchSemanticsNodes().filter { !it.touchBoundsInRoot.isEmpty }  // lazy items prefetched but not placed have empty bounds

fun ComposeContentTestRule.assertNoTargetInCutout() {
    val cutout = cutout()
    for (n in clickables()) assertFalse("click target ${n.touchBoundsInRoot} overlaps the cutout $cutout", n.touchBoundsInRoot.overlaps(cutout))
}

fun ComposeContentTestRule.assertTargetsAtLeast48dp() {
    val d = root().layoutInfo.density.density
    for (n in clickables()) {
        val b = n.touchBoundsInRoot
        assertTrue("click target $b is under 48 dp", b.width >= 48 * d - 1 && b.height >= 48 * d - 1)
    }
}

fun ComposeContentTestRule.assertFitsWithoutScroll() {
    val r = root()
    val d = r.layoutInfo.density.density
    assertEquals("root height", 340 * d, r.size.height.toFloat(), 1f)
    val rootRect = Rect(0f, 0f, r.size.width.toFloat(), r.size.height.toFloat())
    fun walk(n: androidx.compose.ui.semantics.SemanticsNode) {
        assertTrue("node scrolls vertically", n.config.getOrNull(SemanticsProperties.VerticalScrollAxisRange) == null)
        val b = Rect(n.positionInRoot, androidx.compose.ui.geometry.Size(n.size.width.toFloat(), n.size.height.toFloat()))
        assertTrue("node $b leaves the root $rootRect", b.left >= rootRect.left - 1 && b.top >= rootRect.top - 1 && b.right <= rootRect.right + 1 && b.bottom <= rootRect.bottom + 1)
        n.children.forEach(::walk)
    }
    walk(r)
}
