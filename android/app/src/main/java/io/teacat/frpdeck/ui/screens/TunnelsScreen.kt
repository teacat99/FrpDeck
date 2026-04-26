package io.teacat.frpdeck.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.teacat.frpdeck.FrpDeckApp
import io.teacat.frpdeck.R
import io.teacat.frpdeck.core.api.Tunnel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * Tunnel list with inline Start/Stop. Polls every 5 seconds while the
 * tab is on screen — tunnel state changes also stream over the
 * WebSocket bus (ports the desktop UI already), but this Composable is
 * deliberately self-contained for v0.1 to avoid a separate WS reconnect
 * path on mobile.
 */
@Composable
fun TunnelsScreen() {
    val ctx = LocalContext.current
    val app = ctx.applicationContext as FrpDeckApp
    val engine = app.engine

    val running by engine.running.collectAsState()
    val scope = rememberCoroutineScope()

    var tunnels by remember { mutableStateOf<List<Tunnel>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }
    val tick = remember { mutableIntStateOf(0) }

    LaunchedEffect(running, tick.intValue) {
        if (!running) {
            tunnels = emptyList()
            return@LaunchedEffect
        }
        val client = engine.apiClient ?: return@LaunchedEffect
        try {
            tunnels = client.service.listTunnels().items
            error = null
        } catch (t: Throwable) {
            error = t.message
        }
        delay(5000)
        tick.intValue = tick.intValue + 1
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (!running) {
            Text(text = stringResource(R.string.status_stopped))
            return@Column
        }
        if (error != null) {
            Text(text = "Error: $error", color = MaterialTheme.colorScheme.error)
        }
        if (tunnels.isEmpty()) {
            Text(text = stringResource(R.string.empty_tunnels))
        } else {
            LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(tunnels) { t ->
                    TunnelCard(
                        tunnel = t,
                        onStart = {
                            scope.launch {
                                runCatching { engine.apiClient?.service?.startTunnel(t.id) }
                                tick.intValue = tick.intValue + 1
                            }
                        },
                        onStop = {
                            scope.launch {
                                runCatching { engine.apiClient?.service?.stopTunnel(t.id) }
                                tick.intValue = tick.intValue + 1
                            }
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun TunnelCard(tunnel: Tunnel, onStart: () -> Unit, onStop: () -> Unit) {
    val running = tunnel.status == "active"
    Card(modifier = Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.padding(end = 8.dp)) {
                Text(text = tunnel.name, fontWeight = FontWeight.SemiBold)
                Text(
                    text = "${tunnel.type}  •  local:${tunnel.local_port}",
                    style = MaterialTheme.typography.bodySmall,
                )
                Text(
                    text = tunnel.status,
                    style = MaterialTheme.typography.labelSmall,
                    color = statusColor(tunnel.status),
                )
            }
            androidx.compose.foundation.layout.Spacer(Modifier.weight(1f))
            if (running) {
                IconButton(onClick = onStop) {
                    Icon(Icons.Default.Stop, contentDescription = "stop")
                }
            } else {
                IconButton(onClick = onStart) {
                    Icon(Icons.Default.PlayArrow, contentDescription = "start")
                }
            }
        }
    }
}

private fun statusColor(status: String): Color = when (status) {
    "active"   -> Color(0xFF34D399)
    "failed"   -> Color(0xFFEF4444)
    "expired"  -> Color(0xFFF59E0B)
    "stopped"  -> Color(0xFF9CA3AF)
    else       -> Color.Unspecified
}
