package io.teacat.frpdeck.ui.screens

import android.content.Intent
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.teacat.frpdeck.FrpDeckApp
import io.teacat.frpdeck.R
import io.teacat.frpdeck.service.FrpDeckForegroundService
import kotlinx.coroutines.flow.collectLatest

/**
 * Status tab — primary entry point. Shows whether the embedded server
 * is running, exposes the Start/Stop toggle (which routes through the
 * foreground service so the lifecycle survives Activity recreation),
 * and surfaces the rolling log tail emitted by the gomobile bridge.
 */
@Composable
fun StatusScreen() {
    val ctx = LocalContext.current
    val app = ctx.applicationContext as FrpDeckApp
    val engine = app.engine

    val running by engine.running.collectAsState()
    val listenAddr by engine.listenAddr.collectAsState()

    val recentLogs = remember { mutableStateListOf<String>() }
    LaunchedEffect(Unit) {
        engine.logs.collectLatest { line ->
            recentLogs.add(0, line)
            if (recentLogs.size > 200) {
                recentLogs.removeAt(recentLogs.size - 1)
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Card(modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = if (running)
                        stringResource(R.string.status_running, listenAddr.ifEmpty { "—" })
                    else
                        stringResource(R.string.status_stopped),
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.SemiBold,
                )

                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    if (running) {
                        OutlinedButton(onClick = {
                            ctx.startService(Intent(ctx, FrpDeckForegroundService::class.java)
                                .setAction(FrpDeckForegroundService.ACTION_STOP))
                        }) {
                            Icon(Icons.Default.Stop, contentDescription = null)
                            Text(text = "  " + stringResource(R.string.action_stop))
                        }
                    } else {
                        Button(onClick = {
                            ctx.startService(Intent(ctx, FrpDeckForegroundService::class.java)
                                .setAction(FrpDeckForegroundService.ACTION_START))
                        }) {
                            Icon(Icons.Default.PlayArrow, contentDescription = null)
                            Text(text = "  " + stringResource(R.string.action_start))
                        }
                    }
                }

                Text(
                    text = engine.version(),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.6f),
                )
            }
        }

        Card(modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.padding(16.dp)) {
                Text(text = "Logs", style = MaterialTheme.typography.titleMedium)
                Box(
                    modifier = Modifier
                        .padding(top = 8.dp)
                        .fillMaxWidth()
                ) {
                    if (recentLogs.isEmpty()) {
                        Text(
                            text = "—",
                            color = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.5f),
                        )
                    } else {
                        LazyColumn(
                            modifier = Modifier.fillMaxWidth(),
                            contentPadding = PaddingValues(0.dp),
                        ) {
                            items(recentLogs) { line ->
                                Text(
                                    text = line,
                                    fontFamily = FontFamily.Monospace,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = lineColor(line),
                                    modifier = Modifier.padding(vertical = 2.dp),
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun lineColor(line: String): Color {
    return when {
        line.contains("[error]", ignoreCase = true) -> Color(0xFFEF4444)
        line.contains("[warn", ignoreCase = true)   -> Color(0xFFF59E0B)
        line.startsWith("[tunnel")                  -> Color(0xFF60A5FA)
        line.startsWith("[endpoint")                -> Color(0xFF34D399)
        else                                        -> Color.Unspecified
    }
}
