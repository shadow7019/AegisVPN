package net.aegisvpn

import android.content.Context
import android.content.Intent
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.getValue
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * App settings: system kill-switch deep link, Threat Protection, split
 * tunneling, auto-connect on boot, obfuscation, coordination server URL and
 * the client WireGuard identity.
 */
@Composable
internal fun SettingsScreen(store: ProfileStore) {
    val context = LocalContext.current
    var threat by remember { mutableStateOf(store.threatProtection) }
    var obfs by remember { mutableStateOf(store.obfuscationEnabled) }
    var obfsKey by remember { mutableStateOf(store.obfsKey) }
    var autoConnect by remember { mutableStateOf(store.autoConnect) }
    var coordUrl by remember { mutableStateOf(store.coordUrl) }
    var splitCount by remember { mutableStateOf(store.splitPackages.size) }
    var publicKey by remember { mutableStateOf(store.ensureKeypair().second) }
    var showSplitDialog by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(bottom = 28.dp),
    ) {
        Text(
            "Settings",
            modifier = Modifier.padding(start = 20.dp, top = 14.dp, bottom = 8.dp),
            fontSize = 22.sp,
            fontWeight = FontWeight.Bold,
            color = TextPrimary,
        )

        SettingsCard(
            title = "Kill Switch",
            body = "Use Android's system kill switch: under Always-on VPN enable " +
                "\"Block connections without VPN\" so traffic cannot leak if the " +
                "tunnel drops. AegisVPN opens that screen for you.",
        ) {
            Button(onClick = {
                runCatching { context.startActivity(Intent(Settings.ACTION_VPN_SETTINGS)) }
            }) {
                Text("Open VPN settings")
            }
        }

        ToggleCard(
            title = "Threat Protection",
            body = "Filter ads and trackers inside the tunnel via the Aegis DNS " +
                "resolver (${ProfileStore.AEGIS_DNS}).",
            checked = threat,
            onChange = {
                threat = it
                store.threatProtection = it
            },
        )

        SettingsCard(
            title = "Obfuscation",
            body = "Wrap WireGuard UDP packets with the AegisObfs layer (AegisObfs " +
                "profile extensions). Client and node must share the passphrase.",
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    if (obfs) "Enabled" else "Disabled",
                    fontSize = 13.sp,
                    color = if (obfs) Teal else TextSecondary,
                    modifier = Modifier.weight(1f),
                )
                Switch(checked = obfs, onCheckedChange = {
                    obfs = it
                    store.obfuscationEnabled = it
                })
            }
            if (obfs) {
                Spacer(Modifier.height(10.dp))
                OutlinedTextField(
                    value = obfsKey,
                    onValueChange = {
                        obfsKey = it
                        store.obfsKey = it
                    },
                    label = { Text("Obfuscation passphrase") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }

        ToggleCard(
            title = "Auto-connect",
            body = "Bring the tunnel up with the last selected server when the " +
                "device boots.",
            checked = autoConnect,
            onChange = {
                autoConnect = it
                store.autoConnect = it
            },
        )

        SettingsCard(
            title = "Split tunneling",
            body = "Excluded apps use the bare network instead of the VPN " +
                "(addDisallowedApplication).",
        ) {
            Text(
                "$splitCount app${if (splitCount == 1) "" else "s"} excluded",
                fontSize = 13.sp,
                color = TextSecondary,
            )
            Spacer(Modifier.height(10.dp))
            OutlinedButton(onClick = { showSplitDialog = true }) {
                Text("Choose apps")
            }
        }

        SettingsCard(
            title = "Coordination server",
            body = "Source of the node registry (GET /v1/nodes).",
        ) {
            OutlinedTextField(
                value = coordUrl,
                onValueChange = {
                    coordUrl = it
                    store.coordUrl = it
                },
                label = { Text("Node registry URL") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
        }

        SettingsCard(
            title = "WireGuard identity",
            body = "The client private key is generated on this device and kept in " +
                "encrypted preferences; it never leaves the phone. Regenerating " +
                "also rebuilds the current profile.",
        ) {
            Text(
                publicKey,
                fontSize = 12.sp,
                fontFamily = FontFamily.Monospace,
                color = TextSecondary,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(10.dp))
            OutlinedButton(onClick = {
                publicKey = store.regenerateKeypair().second
                store.selectedNode?.let { store.selectNode(it) }
            }) {
                Text("Regenerate keypair")
            }
        }
    }

    if (showSplitDialog) {
        SplitTunnelDialog(
            initial = store.splitPackages,
            onDismiss = { showSplitDialog = false },
            onSave = {
                store.splitPackages = it
                splitCount = it.size
                showSplitDialog = false
            },
        )
    }
}

@Composable
private fun SettingsCard(
    title: String,
    body: String,
    content: (@Composable () -> Unit)? = null,
) {
    Card(
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Navy850),
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp),
    ) {
        Column(Modifier.padding(16.dp)) {
            Text(title, fontWeight = FontWeight.Bold, color = TextPrimary)
            Spacer(Modifier.height(4.dp))
            Text(body, fontSize = 13.sp, color = TextSecondary)
            content?.let {
                Spacer(Modifier.height(12.dp))
                it()
            }
        }
    }
}

@Composable
private fun ToggleCard(
    title: String,
    body: String,
    checked: Boolean,
    onChange: (Boolean) -> Unit,
) {
    Card(
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Navy850),
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp),
    ) {
        Row(
            Modifier.padding(horizontal = 16.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(title, fontWeight = FontWeight.Bold, color = TextPrimary)
                Spacer(Modifier.height(4.dp))
                Text(body, fontSize = 13.sp, color = TextSecondary)
            }
            Spacer(Modifier.width(12.dp))
            Switch(checked = checked, onCheckedChange = onChange)
        }
    }
}

// ------------------------------------------------------------ split tunneling

private data class AppEntry(val packageName: String, val label: String)

@Composable
private fun SplitTunnelDialog(
    initial: Set<String>,
    onDismiss: () -> Unit,
    onSave: (Set<String>) -> Unit,
) {
    val context = LocalContext.current
    val apps = remember { loadLaunchableApps(context) }
    var selected by remember { mutableStateOf(initial) }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Exclude apps") },
        text = {
            if (apps.isEmpty()) {
                Text("No launchable apps found.", color = TextSecondary)
            } else {
                LazyColumn(Modifier.fillMaxWidth().heightIn(max = 420.dp)) {
                    items(apps.size) { index ->
                        val app = apps[index]
                        val checked = app.packageName in selected
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    selected = if (checked) selected - app.packageName
                                    else selected + app.packageName
                                }
                                .padding(vertical = 2.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Checkbox(
                                checked = checked,
                                onCheckedChange = { now ->
                                    selected = if (now) selected + app.packageName
                                    else selected - app.packageName
                                },
                            )
                            Column(Modifier.weight(1f)) {
                                Text(app.label, fontSize = 14.sp, color = TextPrimary)
                                Text(
                                    app.packageName,
                                    fontSize = 11.sp,
                                    color = TextSecondary,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(onClick = { onSave(selected) }) { Text("Save") }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("Cancel") }
        },
        containerColor = Navy900,
    )
}

/**
 * Launchable apps for the exclusion picker. Uses the manifest <queries>
 * launchable-intent declaration (no QUERY_ALL_PACKAGES needed).
 */
internal fun loadLaunchableApps(context: Context): List<AppEntry> {
    val pm = context.packageManager
    val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
    return pm.queryIntentActivities(launcher, 0)
        .asSequence()
        .map { it.activityInfo }
        .distinctBy { it.packageName }
        .filter { it.packageName != context.packageName }
        .map { AppEntry(it.packageName, it.loadLabel(pm).toString()) }
        .sortedBy { it.label.lowercase() }
        .toList()
}
