package io.teacat.frpdeck.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.teacat.frpdeck.FrpDeckApp
import io.teacat.frpdeck.R
import io.teacat.frpdeck.core.api.Endpoint

/**
 * Read-only list of endpoints — the mobile UX intentionally keeps
 * create/edit on the WebView (it has the full form + validation already
 * built for desktop). Tapping an endpoint will open the WebView at
 * `/endpoints/<id>` in a follow-up polish.
 */
@Composable
fun EndpointsScreen() {
    val ctx = LocalContext.current
    val app = ctx.applicationContext as FrpDeckApp
    val engine = app.engine

    val running by engine.running.collectAsState()

    var endpoints by remember { mutableStateOf<List<Endpoint>>(emptyList()) }
    var error by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(running) {
        if (!running) {
            endpoints = emptyList()
            return@LaunchedEffect
        }
        try {
            val client = engine.apiClient ?: return@LaunchedEffect
            endpoints = client.service.listEndpoints().items
            error = null
        } catch (t: Throwable) {
            error = t.message
        }
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
        if (endpoints.isEmpty()) {
            Text(text = stringResource(R.string.empty_endpoints))
        } else {
            LazyColumn(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                items(endpoints) { ep ->
                    EndpointCard(ep)
                }
            }
        }
    }
}

@Composable
private fun EndpointCard(ep: Endpoint) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(12.dp)) {
            Text(text = ep.name, fontWeight = FontWeight.SemiBold)
            Text(
                text = "${ep.addr}:${ep.port}  •  ${ep.protocol ?: "tcp"}",
                style = MaterialTheme.typography.bodySmall,
            )
            Text(
                text = if (ep.enabled) "enabled" else "disabled",
                style = MaterialTheme.typography.labelSmall,
                color = if (ep.enabled)
                    MaterialTheme.colorScheme.primary
                else
                    MaterialTheme.colorScheme.onSurface.copy(alpha = 0.5f),
            )
        }
    }
}
