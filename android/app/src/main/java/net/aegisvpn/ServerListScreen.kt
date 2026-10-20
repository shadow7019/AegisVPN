package net.aegisvpn

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * Node list from the coordination server (docs/PROTOCOL.md section 4).
 * OkHttp + org.json keep the dependency surface light; nodes are grouped by
 * country with load bars and type badges. Tapping a node builds and stores the
 * wg-quick profile (ProfileStore.selectNode).
 */
@Composable
internal fun ServerListScreen(store: ProfileStore, onSelected: () -> Unit) {
    var nodes by remember { mutableStateOf<List<Node>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var reloadKey by remember { mutableIntStateOf(0) }
    var selectedId by remember { mutableStateOf<String?>(null) }

    LaunchedEffect(Unit) {
        selectedId = store.selectedNode?.id
    }

    LaunchedEffect(reloadKey) {
        error = null
        nodes = null
        try {
            nodes = withContext(Dispatchers.IO) { fetchNodes(store.coordUrl) }
        } catch (e: Exception) {
            error = e.message ?: "Failed to reach the coordination server"
        }
    }

    Column(Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 20.dp, end = 8.dp, top = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                "Servers",
                fontSize = 22.sp,
                fontWeight = FontWeight.Bold,
                color = TextPrimary,
                modifier = Modifier.weight(1f),
            )
            IconButton(onClick = { reloadKey++ }) {
                Icon(Icons.Filled.Refresh, contentDescription = "Refresh", tint = Teal)
            }
        }

        when {
            error != null -> ErrorPane(error!!, onRetry = { reloadKey++ })
            nodes == null -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = Teal)
            }
            else -> NodeList(
                nodes = nodes.orEmpty(),
                selectedId = selectedId,
                onSelect = { node ->
                    store.selectNode(node)
                    selectedId = node.id
                    onSelected()
                },
            )
        }
    }
}

@Composable
private fun NodeList(nodes: List<Node>, selectedId: String?, onSelect: (Node) -> Unit) {
    val grouped = nodes.groupBy { it.country }.toSortedMap()
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(bottom = 24.dp),
    ) {
        grouped.forEach { (country, countryNodes) ->
            item(key = "header_$country") {
                Text(
                    text = "$country · ${countryNodes.size} node${if (countryNodes.size == 1) "" else "s"}",
                    modifier = Modifier.padding(start = 20.dp, top = 18.dp, bottom = 8.dp),
                    fontSize = 13.sp,
                    fontWeight = FontWeight.Bold,
                    color = TextSecondary,
                )
            }
            countryNodes.sortedBy { it.load }.forEach { node ->
                item(key = node.id) {
                    NodeRow(node = node, selected = node.id == selectedId, onSelect = onSelect)
                }
            }
        }
        if (nodes.isEmpty()) {
            item(key = "empty") {
                Text(
                    "No nodes reported yet.",
                    modifier = Modifier.padding(20.dp),
                    color = TextSecondary,
                )
            }
        }
    }
}

@Composable
private fun NodeRow(node: Node, selected: Boolean, onSelect: (Node) -> Unit) {
    Card(
        shape = RoundedCornerShape(14.dp),
        colors = CardDefaults.cardColors(containerColor = if (selected) Navy800 else Navy850),
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 5.dp),
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .clickable { onSelect(node) }
                .padding(14.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(node.city, fontWeight = FontWeight.SemiBold, color = TextPrimary)
                        if (selected) {
                            Spacer(Modifier.width(8.dp))
                            Text(
                                "SELECTED",
                                fontSize = 10.sp,
                                fontWeight = FontWeight.Bold,
                                color = Teal,
                            )
                        }
                    }
                    Text(node.endpoint, fontSize = 12.sp, color = TextSecondary)
                }
                Text("${node.latencyMs} ms", fontSize = 12.sp, color = Teal)
            }
            Spacer(Modifier.height(10.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                LinearProgressIndicator(
                    progress = { node.load.toFloat().coerceIn(0f, 1f) },
                    modifier = Modifier
                        .weight(1f)
                        .height(5.dp),
                    color = loadColor(node.load),
                    trackColor = Navy700,
                )
                Spacer(Modifier.width(8.dp))
                Text("${(node.load * 100).toInt()}% load", fontSize = 11.sp, color = TextSecondary)
                node.badgeLabel?.let { badge ->
                    Spacer(Modifier.width(8.dp))
                    Box(
                        modifier = Modifier
                            .background(Teal.copy(alpha = 0.14f), RoundedCornerShape(6.dp))
                            .padding(horizontal = 8.dp, vertical = 3.dp),
                    ) {
                        Text(
                            badge,
                            fontSize = 10.sp,
                            fontWeight = FontWeight.Bold,
                            color = Teal,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ErrorPane(message: String, onRetry: () -> Unit) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
            modifier = Modifier.padding(24.dp),
        ) {
            Text("Could not load servers", fontWeight = FontWeight.Bold, color = TextPrimary)
            Spacer(Modifier.height(8.dp))
            Text(message, color = TextSecondary, textAlign = TextAlign.Center)
            Spacer(Modifier.height(16.dp))
            androidx.compose.material3.Button(onClick = onRetry) { Text("Retry") }
        }
    }
}

private fun loadColor(load: Double): Color = when {
    load < 0.60 -> Teal
    load < 0.85 -> Color(0xFFF2B33D)
    else -> AlertRed
}

/** Blocking registry fetch — call from a background dispatcher only. */
private fun fetchNodes(coordUrl: String): List<Node> {
    val client = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(10, TimeUnit.SECONDS)
        .build()
    val request = Request.Builder()
        .url(coordUrl)
        .header("Accept", "application/json")
        .build()
    client.newCall(request).execute().use { response ->
        if (!response.isSuccessful) {
            throw IOException("Coordination server returned HTTP ${response.code}")
        }
        val body = response.body?.string().orEmpty()
        if (body.isBlank()) {
            throw IOException("Empty response from coordination server")
        }
        return NodesResponse.fromJson(body).nodes
    }
}
