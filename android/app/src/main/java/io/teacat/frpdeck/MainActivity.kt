package io.teacat.frpdeck

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Cable
import androidx.compose.material.icons.filled.MoreHoriz
import androidx.compose.material.icons.filled.Power
import androidx.compose.material.icons.filled.Public
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.core.content.ContextCompat
import io.teacat.frpdeck.ui.screens.EndpointsScreen
import io.teacat.frpdeck.ui.screens.MoreScreen
import io.teacat.frpdeck.ui.screens.StatusScreen
import io.teacat.frpdeck.ui.screens.TunnelsScreen
import io.teacat.frpdeck.ui.theme.FrpDeckTheme

/**
 * Single-activity host. Bottom navigation switches between the four
 * tabs (Status / Endpoints / Tunnels / More). Each tab is its own
 * Composable that pulls data via [FrpDeckApp.engine] when the engine is
 * running.
 *
 * Configuration changes (rotation, locale, layout direction) are
 * declared in the manifest as `configChanges` so the Activity is NOT
 * recreated; the Compose tree handles everything itself.
 */
class MainActivity : ComponentActivity() {

    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { /* result ignored — the foreground notification is decorative. */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestNotificationPermissionIfNeeded()

        setContent {
            FrpDeckTheme {
                FrpDeckRoot()
            }
        }
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = ContextCompat.checkSelfPermission(
                this,
                Manifest.permission.POST_NOTIFICATIONS,
            ) == PackageManager.PERMISSION_GRANTED
            if (!granted) {
                notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@androidx.compose.runtime.Composable
private fun FrpDeckRoot() {
    var selectedTab by rememberSaveable { mutableStateOf(0) }

    val tabs = remember {
        listOf(
            TabSpec(R.string.tab_status, Icons.Default.Power),
            TabSpec(R.string.tab_endpoints, Icons.Default.Cable),
            TabSpec(R.string.tab_tunnels, Icons.Default.Public),
            TabSpec(R.string.tab_more, Icons.Default.MoreHoriz),
        )
    }

    Scaffold(
        bottomBar = {
            NavigationBar {
                tabs.forEachIndexed { idx, tab ->
                    val labelRes = tab.titleRes
                    NavigationBarItem(
                        selected = selectedTab == idx,
                        onClick = { selectedTab = idx },
                        icon = { Icon(tab.icon, contentDescription = null) },
                        label = { Text(text = androidx.compose.ui.res.stringResource(labelRes)) },
                    )
                }
            }
        },
    ) { padding ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            when (selectedTab) {
                0 -> StatusScreen()
                1 -> EndpointsScreen()
                2 -> TunnelsScreen()
                3 -> MoreScreen()
            }
        }
    }
}

private data class TabSpec(
    val titleRes: Int,
    val icon: androidx.compose.ui.graphics.vector.ImageVector,
)
