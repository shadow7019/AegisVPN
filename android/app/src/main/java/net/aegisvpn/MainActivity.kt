package net.aegisvpn

import android.Manifest
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Place
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import java.util.Locale

// ---------------------------------------------------------------------------
// Palette: deep navy + teal accent (#17C7BD). No blue/indigo anywhere.
// ---------------------------------------------------------------------------
internal val Navy950 = Color(0xFF0A1220)
internal val Navy900 = Color(0xFF0E1726)
internal val Navy850 = Color(0xFF121D31)
internal val Navy800 = Color(0xFF16233A)
internal val Navy700 = Color(0xFF1E2F4D)
internal val Teal = Color(0xFF17C7BD)
internal val TealDim = Color(0xFF0F948C)
internal val TextPrimary = Color(0xFFE6EEF8)
internal val TextSecondary = Color(0xFF8CA3C3)
internal val AlertRed = Color(0xFFE5646E)

private val AegisColorScheme = darkColorScheme(
    primary = Teal,
    onPrimary = Navy950,
    secondary = TealDim,
    onSecondary = Navy950,
    background = Navy950,
    onBackground = TextPrimary,
    surface = Navy900,
    onSurface = TextPrimary,
    surfaceVariant = Navy800,
    onSurfaceVariant = TextSecondary,
    outline = Navy700,
    error = AlertRed,
    onError = Navy950,
)

@Composable
internal fun AegisTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = AegisColorScheme, typography = Typography(), content = content)
}

internal sealed interface Screen {
    data object Home : Screen
    data object Servers : Screen
    data object Settings : Screen
}

// ---------------------------------------------------------------------------

class MainActivity : ComponentActivity() {

    private var pendingVpnStart: (() -> Unit)? = null

    private val vpnConsentLauncher =
        registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
            val start = pendingVpnStart
            pendingVpnStart = null
            if (result.resultCode == RESULT_OK) start?.invoke()
        }

    private val notificationPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) {
            // Best effort only; the foreground notification simply stays silent
            // when POST_NOTIFICATIONS is denied on API 33+.
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (Build.VERSION.SDK_INT >= 33) {
            notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
        setContent {
            AegisTheme {
                AegisApp(activity = this)
            }
        }
    }

    /** Runs [start] once VpnService consent is granted (asks first if needed). */
    fun requestVpnConsentThen(start: () -> Unit) {
        val consent = VpnService.prepare(this)
        if (consent == null) {
            start()
        } else {
            pendingVpnStart = start
            vpnConsentLauncher.launch(consent)
        }
    }
}

internal fun launchVpn(context: Context, store: ProfileStore) {
    // Rebuild the profile from the selected node so it always reflects the
    // current keypair + obfuscation settings before the service reads it.
    store.selectedNode?.let { store.selectNode(it) }
    if (store.lastProfile.isNullOrBlank()) {
        Toast.makeText(context, "Select a server first", Toast.LENGTH_LONG).show()
        return
    }
    context.startService(AegisVpnService.connectIntent(context, store))
}

internal fun stopVpn(context: Context) {
    context.startService(AegisVpnService.disconnectIntent(context))
}

// ---------------------------------------------------------------------------

@Composable
internal fun AegisApp(activity: MainActivity) {
    val context = LocalContext.current
    val store = remember { ProfileStore(context) }
    var screen by remember { mutableStateOf<Screen>(Screen.Home) }
    val status by VpnStateBus.status.collectAsStateWithLifecycle()

    val onConnect: () -> Unit = {
        if (store.lastProfile.isNullOrBlank() && store.selectedNode == null) {
            Toast.makeText(context, "Choose a server to build your profile", Toast.LENGTH_LONG).show()
            screen = Screen.Servers
        } else {
            activity.requestVpnConsentThen { launchVpn(context, store) }
        }
    }
    val onDisconnect: () -> Unit = { stopVpn(context) }

    Scaffold(
        containerColor = MaterialTheme.colorScheme.background,
        bottomBar = {
            NavigationBar(containerColor = Navy900) {
                NavigationBarItem(
                    selected = screen == Screen.Home,
                    onClick = { screen = Screen.Home },
                    icon = { Icon(Icons.Filled.Home, contentDescription = null) },
                    label = { Text("Home") },
                )
                NavigationBarItem(
                    selected = screen == Screen.Servers,
                    onClick = { screen = Screen.Servers },
                    icon = { Icon(Icons.Filled.Place, contentDescription = null) },
                    label = { Text("Servers") },
                )
                NavigationBarItem(
                    selected = screen == Screen.Settings,
                    onClick = { screen = Screen.Settings },
                    icon = { Icon(Icons.Filled.Settings, contentDescription = null) },
                    label = { Text("Settings") },
                )
            }
        },
    ) { innerPadding ->
        Box(Modifier.padding(innerPadding)) {
            when (screen) {
                Screen.Home -> HomeScreen(
                    status = status,
                    node = store.selectedNode,
                    onConnect = onConnect,
                    onDisconnect = onDisconnect,
                    onChooseServer = { screen = Screen.Servers },
                )
                Screen.Servers -> ServerListScreen(store = store, onSelected = { screen = Screen.Home })
                Screen.Settings -> SettingsScreen(store = store)
            }
        }
    }
}

// --------------------------------------------------------------- home screen

@Composable
internal fun HomeScreen(
    status: TunnelStatus,
    node: Node?,
    onConnect: () -> Unit,
    onDisconnect: () -> Unit,
    onChooseServer: () -> Unit,
) {
    val active = status.state == "connected" || status.state == "connecting"
    val now = remember(active) { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(active) {
        if (active) {
            while (true) {
                delay(1000)
                now.value = System.currentTimeMillis()
            }
        }
    }
    val durationMs =
        if (active && status.connectedAtMillis > 0) {
            (now.value - status.connectedAtMillis).coerceAtLeast(0L)
        } else {
            0L
        }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.height(28.dp))

        // Big circular connect button with teal glow rings.
        Box(contentAlignment = Alignment.Center, modifier = Modifier.size(250.dp)) {
            Canvas(Modifier.fillMaxSize()) {
                val radius = size.minDimension / 2f
                drawCircle(
                    color = if (active) Teal.copy(alpha = 0.30f) else Navy700,
                    radius = radius,
                    style = Stroke(width = 5.dp.toPx()),
                )
                drawCircle(
                    color = if (active) Teal.copy(alpha = 0.12f) else Navy800,
                    radius = radius * 0.86f,
                    style = Stroke(width = 2.dp.toPx()),
                )
            }
            Button(
                onClick = { if (active) onDisconnect() else onConnect() },
                shape = CircleShape,
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (active) Teal else Navy800,
                    contentColor = if (active) Navy950 else TextPrimary,
                ),
                modifier = Modifier.size(184.dp),
            ) {
                Text(
                    text = if (active) "DISCONNECT" else "CONNECT",
                    textAlign = TextAlign.Center,
                    fontWeight = FontWeight.Bold,
                    fontSize = 17.sp,
                )
            }
        }

        Spacer(Modifier.height(18.dp))
        Text(
            text = stateLabel(status.state),
            fontSize = 22.sp,
            fontWeight = FontWeight.Bold,
            color = if (status.state == "connected") Teal else TextSecondary,
        )
        if (status.error.isNotBlank()) {
            Spacer(Modifier.height(6.dp))
            Text(
                text = status.error,
                color = AlertRed,
                fontSize = 13.sp,
                textAlign = TextAlign.Center,
            )
        }

        Spacer(Modifier.height(18.dp))
        if (node != null) {
            NodeCard(node = node, status = status)
        } else {
            NoServerCard(onBrowse = onChooseServer)
        }

        Spacer(Modifier.height(16.dp))
        // Live stats row: rx / tx / duration.
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            StatCard("DOWNLOAD", formatBytes(status.rxBytes), Modifier.weight(1f))
            StatCard("UPLOAD", formatBytes(status.txBytes), Modifier.weight(1f))
            StatCard("TIME", formatDuration(durationMs), Modifier.weight(1f))
        }

        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun NodeCard(node: Node, status: TunnelStatus) {
    Card(
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Navy850),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier.size(46.dp).background(Navy800, CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = node.country.take(2).uppercase(Locale.US),
                    color = Teal,
                    fontWeight = FontWeight.Bold,
                )
            }
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    text = "${node.city}, ${node.country}",
                    fontWeight = FontWeight.Bold,
                    color = TextPrimary,
                )
                Text(
                    text = status.endpoint.ifBlank { node.endpoint },
                    fontSize = 12.sp,
                    color = TextSecondary,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Column(horizontalAlignment = Alignment.End) {
                Text(
                    text = if (status.state == "connected") "ENCRYPTED" else "READY",
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Bold,
                    color = Teal,
                )
                if (status.latencyMs > 0) {
                    Text("${status.latencyMs} ms", fontSize = 11.sp, color = TextSecondary)
                }
            }
        }
    }
}

@Composable
private fun NoServerCard(onBrowse: () -> Unit) {
    Card(
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Navy850),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(Modifier.padding(16.dp)) {
            Text("No server selected", fontWeight = FontWeight.Bold, color = TextPrimary)
            Spacer(Modifier.height(4.dp))
            Text(
                "Pick an exit node to generate your WireGuard profile.",
                fontSize = 13.sp,
                color = TextSecondary,
            )
            Spacer(Modifier.height(12.dp))
            OutlinedButton(onClick = onBrowse) { Text("Browse servers") }
        }
    }
}

@Composable
private fun StatCard(label: String, value: String, modifier: Modifier = Modifier) {
    Card(
        shape = RoundedCornerShape(14.dp),
        colors = CardDefaults.cardColors(containerColor = Navy850),
        modifier = modifier,
    ) {
        Column(
            Modifier.padding(horizontal = 10.dp, vertical = 12.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(label, fontSize = 10.sp, fontWeight = FontWeight.Bold, color = TextSecondary)
            Spacer(Modifier.height(4.dp))
            Text(
                value,
                fontSize = 15.sp,
                fontWeight = FontWeight.Bold,
                color = Teal,
                maxLines = 1,
            )
        }
    }
}

// ------------------------------------------------------------------ helpers

internal fun stateLabel(state: String): String = when (state) {
    "connected" -> "Protected"
    "connecting" -> "Connecting…"
    "disconnecting" -> "Disconnecting…"
    else -> "Unprotected"
}

private fun formatBytes(bytes: Long): String {
    if (bytes < 0) return "0 B"
    if (bytes < 1024) return "$bytes B"
    val kb = bytes / 1024.0
    if (kb < 1024) return String.format(Locale.US, "%.1f KB", kb)
    val mb = kb / 1024.0
    if (mb < 1024) return String.format(Locale.US, "%.1f MB", mb)
    return String.format(Locale.US, "%.1f GB", mb / 1024.0)
}

private fun formatDuration(ms: Long): String {
    if (ms <= 0) return "--:--:--"
    val totalSeconds = ms / 1000
    return String.format(
        Locale.US,
        "%02d:%02d:%02d",
        totalSeconds / 3600,
        (totalSeconds % 3600) / 60,
        totalSeconds % 60,
    )
}
