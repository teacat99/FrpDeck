package io.teacat.frpdeck.ui.screens

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.teacat.frpdeck.FrpDeckApp
import io.teacat.frpdeck.R
import io.teacat.frpdeck.backup.BackupBundle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * "More" tab. The first section opens the embedded WebView for
 * advanced features that we don't reimplement in Compose. The second
 * section exposes the SAF backup contract so users can round-trip the
 * sqlite + settings folder to/from any storage provider (Drive, USB,
 * shared drive, …).
 *
 * Backup intentionally stops at the engine boundary: the foreground
 * service must be stopped before importing, otherwise the new sqlite
 * file fights the live one.
 */
@Composable
fun MoreScreen() {
    val ctx = LocalContext.current
    val app = ctx.applicationContext as FrpDeckApp
    val engine = app.engine
    val scope = rememberCoroutineScope()

    val running by engine.running.collectAsState()
    val listenAddr by engine.listenAddr.collectAsState()

    var openRoute by remember { mutableStateOf<String?>(null) }
    var lastBackupMessage by remember { mutableStateOf("") }

    val exportLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.CreateDocument("application/zip")
    ) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            val bytes = withContext(Dispatchers.IO) {
                runCatching { BackupBundle.export(ctx, uri) }
            }
            lastBackupMessage = bytes.fold(
                onSuccess = { "Exported $it bytes" },
                onFailure = { "Export failed: ${it.message}" },
            )
        }
    }

    val importLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument()
    ) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        scope.launch {
            val n = withContext(Dispatchers.IO) {
                runCatching {
                    engine.stop()
                    BackupBundle.import(ctx, uri)
                }
            }
            lastBackupMessage = n.fold(
                onSuccess = { "Restored $it files. Restart engine to load." },
                onFailure = { "Import failed: ${it.message}" },
            )
        }
    }

    if (openRoute != null && running && listenAddr.isNotEmpty()) {
        WebViewScreen(
            url = "http://$listenAddr/$openRoute",
            token = engine.adminToken(),
            onBack = { openRoute = null },
        )
        return
    }

    val rows = listOf(
        "settings"  to stringResource(R.string.more_settings),
        "audit"     to stringResource(R.string.more_history),
        "profiles"  to stringResource(R.string.more_profiles),
        "remote"    to stringResource(R.string.more_remote),
        "tunnels?frpsHelper=1" to stringResource(R.string.more_frps_helper),
    )

    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (running) {
            items(rows) { (route, label) ->
                Card(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { openRoute = route },
                ) {
                    Column(modifier = Modifier.padding(16.dp)) {
                        Text(text = label, fontWeight = FontWeight.SemiBold)
                        Text(
                            text = "/$route",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                        )
                    }
                }
            }
        } else {
            item { Text(text = stringResource(R.string.status_stopped)) }
        }

        item { HorizontalDivider(modifier = Modifier.padding(vertical = 8.dp)) }

        item {
            Card(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { exportLauncher.launch("frpdeck-backup.zip") },
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text(
                        text = stringResource(R.string.action_export_backup),
                        fontWeight = FontWeight.SemiBold,
                    )
                    Text(
                        text = stringResource(R.string.backup_export_hint),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                    )
                }
            }
        }

        item {
            Card(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { importLauncher.launch(arrayOf("application/zip")) },
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text(
                        text = stringResource(R.string.action_import_backup),
                        fontWeight = FontWeight.SemiBold,
                    )
                    Text(
                        text = stringResource(R.string.backup_import_hint),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                    )
                }
            }
        }

        if (lastBackupMessage.isNotEmpty()) {
            item {
                Text(
                    text = lastBackupMessage,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
        }
    }
}
