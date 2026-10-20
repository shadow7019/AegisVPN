package net.aegisvpn

import aegislib.Aegislib
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Snapshot of the engine status shared with the Compose UI. Field names mirror
 * the serialized aegis.Status struct from docs/PROTOCOL.md section 1.
 */
data class TunnelStatus(
    val state: String = "disconnected", // disconnected|connecting|connected|disconnecting
    val node: String = "",
    val localIp: String = "",
    val endpoint: String = "",
    val rxBytes: Long = 0,
    val txBytes: Long = 0,
    val handshakeAt: Long = 0,
    val latencyMs: Int = 0,
    val error: String = "",
    val connectedAtMillis: Long = 0,
)

/** Process-wide bus so MainActivity can observe the tunnel without re-binding. */
object VpnStateBus {
    val status = MutableStateFlow(TunnelStatus())

    fun markConnecting(node: String) {
        status.value = TunnelStatus(state = "connecting", node = node)
    }

    fun markConnected(node: String) {
        status.value = status.value.copy(
            state = "connected",
            node = node,
            connectedAtMillis = System.currentTimeMillis(),
            error = "",
        )
    }

    fun markDisconnected() {
        status.value = TunnelStatus()
    }

    fun pushError(message: String) {
        status.value = status.value.copy(state = "disconnected", error = message, connectedAtMillis = 0)
    }

    /** Parses the JSON produced by aegislib.StatusJSON(). */
    fun applyJson(text: String) {
        val o = JSONObject(text)
        val cur = status.value
        status.value = cur.copy(
            state = o.optString("state", cur.state).ifBlank { cur.state },
            node = o.optString("node", cur.node).ifBlank { cur.node },
            localIp = o.optString("localIp", cur.localIp).ifBlank { cur.localIp },
            endpoint = o.optString("endpoint", cur.endpoint).ifBlank { cur.endpoint },
            rxBytes = o.optLong("rxBytes", cur.rxBytes),
            txBytes = o.optLong("txBytes", cur.txBytes),
            handshakeAt = o.optLong("handshakeAt", cur.handshakeAt),
            latencyMs = o.optInt("latencyMs", cur.latencyMs),
            error = o.optString("error", cur.error),
        )
    }
}

/**
 * Single seam over the gomobile binding (package `aegislib`, class `Aegislib`
 * — gomobile names the Java class after the Go package). If the engine agent
 * ever builds with -javapkg, only this object's import changes.
 *
 * Java surface (docs/PROTOCOL.md "Android gomobile binding"):
 *   void Start(long fd, String wgConfig, boolean obfsEnabled, String obfsKey) throws Exception
 *   void Stop() throws Exception
 *   String StatusJSON()
 *   String Version()
 */
object AegisEngine {
    fun start(fd: Int, wgConfig: String, obfsEnabled: Boolean, obfsKey: String) {
        Aegislib.Start(fd.toLong(), wgConfig, obfsEnabled, obfsKey)
    }

    fun stop() {
        Aegislib.Stop()
    }

    fun statusJson(): String = Aegislib.StatusJSON()

    fun version(): String = Aegislib.Version()
}

/**
 * The REAL tunnel service.
 *
 * Data flow (docs/PROTOCOL.md section 5, Android column):
 *   1. wg-quick profile + options arrive via intent extras.
 *   2. This side owns routes/DNS/MTU: VpnService.Builder -> establish() -> tun fd.
 *   3. aegislib.Start(fd, wgConfig, obfs, obfsKey) drives wireguard-go over that
 *      fd (tun.CreateTUNFromFile) on a background thread.
 *   4. StatusJSON() is polled every second into [VpnStateBus].
 *
 * Kill switch: Android's supported mechanism is the system setting
 * "Block connections without VPN" (Always-on VPN). A third-party app cannot
 * install firewall rules without root, so SettingsScreen deep-links to
 * Settings.ACTION_VPN_SETTINGS where the user flips the system kill switch.
 */
class AegisVpnService : VpnService() {

    private var tunFd: ParcelFileDescriptor? = null
    private var pollJob: Job? = null
    private val running = AtomicBoolean(false)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    override fun onCreate() {
        super.onCreate()
        ensureChannel(this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Foreground first: when started via startForegroundService (boot or app)
        // startForeground must happen within the timeout window.
        startAsForeground(getString(R.string.notif_connecting))

        if (intent?.action == ACTION_DISCONNECT) {
            shutdown()
            stopSelf()
            return START_NOT_STICKY
        }

        val profile = intent?.getStringExtra(EXTRA_PROFILE).orEmpty().trim()
        if (profile.isEmpty()) {
            Log.w(TAG, "No profile in start intent; stopping.")
            stopSelf()
            return START_NOT_STICKY
        }

        if (VpnService.prepare(this) != null) {
            Log.e(TAG, "VPN consent not granted; stopping.")
            VpnStateBus.pushError("VPN permission missing")
            stopSelf()
            return START_NOT_STICKY
        }

        if (running.get()) shutdown() // restart with a fresh profile

        val nodeName = intent?.getStringExtra(EXTRA_NODE).orEmpty().ifBlank { "AegisVPN" }
        val obfsEnabled = intent?.getBooleanExtra(EXTRA_OBFS, false) ?: false
        val obfsKey = intent?.getStringExtra(EXTRA_OBFS_KEY).orEmpty()
        val threatProtection = intent?.getBooleanExtra(EXTRA_THREAT, false) ?: false
        val splitPackages = intent?.getStringArrayExtra(EXTRA_SPLIT_PACKAGES)?.toList().orEmpty()

        val config = parseWgQuick(profile)
        val tun = try {
            establishTun(config, threatProtection, splitPackages, nodeName)
        } catch (e: Exception) {
            Log.e(TAG, "TUN setup failed", e)
            VpnStateBus.pushError(e.message ?: "TUN setup failed")
            stopSelf()
            return START_NOT_STICKY
        }
        tunFd = tun
        running.set(true)
        VpnStateBus.markConnecting(nodeName)

        pollJob = scope.launch {
            try {
                // Real WireGuard: hand the tun fd + wg-quick text (incl. AegisObfs
                // extensions) to the gomobile engine.
                AegisEngine.start(tun.fd, profile, obfsEnabled, obfsKey)
                VpnStateBus.markConnected(nodeName)
                updateNotification("$nodeName · protected")
                pollLoop(nodeName)
            } catch (e: Exception) {
                Log.e(TAG, "aegislib.Start failed", e)
                VpnStateBus.pushError(e.message ?: "engine failed to start")
                shutdown()
                stopSelf()
            }
        }
        return START_NOT_STICKY
    }

    /** User revoked VPN consent (or another VPN took over). */
    override fun onRevoke() {
        shutdown()
        stopSelf()
    }

    override fun onDestroy() {
        shutdown()
        scope.cancel()
        super.onDestroy()
    }

    // ------------------------------------------------------------------ TUN

    /**
     * Builds the Android TUN and returns the fd for aegislib.
     *
     * This side owns the VpnService.Builder (addresses, routes incl.
     * 0.0.0.0/0 + ::/0 for full tunnel, DNS, MTU, session); the Go side owns
     * the WireGuard device itself.
     *
     * ProtectFd note: official wireguard-android protects the WireGuard UDP
     * socket with a JNI back-call into VpnService.protect(fd) made from Go.
     * The current aegislib surface (Start/Stop/StatusJSON/Version) has no such
     * back-call yet; until aegislib grows a ProtectFd hook, the Go dialer rides
     * the default network and setUnderlyingNetworks(null) keeps the TUN alive
     * across network switches. This is the one gap vs wireguard-android to
     * close for production hardening.
     */
    private fun establishTun(
        config: WgQuickConfig,
        threatProtection: Boolean,
        splitPackages: List<String>,
        session: String,
    ): ParcelFileDescriptor {
        val builder = Builder()

        val addresses = config.addresses.ifEmpty { listOf(ProfileStore.CLIENT_TUN_ADDRESS) }
        addresses.forEach { cidr ->
            val (ip, prefix) = splitCidr(cidr)
            builder.addAddress(ip, prefix)
        }

        val routes = config.allowedIps.ifEmpty { listOf("0.0.0.0/0", "::/0") }
        routes.forEach { cidr ->
            val (ip, prefix) = splitCidr(cidr)
            builder.addRoute(ip, prefix)
        }

        // DNS takeover via addDnsServer. Threat Protection routes DNS through the
        // Aegis filtering resolver; fall back to it when the profile has no DNS.
        val dnsServers = buildList {
            addAll(config.dnsServers)
            if (threatProtection && !contains(ProfileStore.AEGIS_DNS)) add(ProfileStore.AEGIS_DNS)
        }.ifEmpty { listOf(ProfileStore.AEGIS_DNS) }
        dnsServers.forEach { builder.addDnsServer(it) }

        builder.setMtu(config.mtu ?: ProfileStore.DEFAULT_MTU)
        builder.setSession(session)

        // Split tunneling: excluded apps bypass the VPN entirely.
        splitPackages.forEach { pkg ->
            runCatching { builder.addDisallowedApplication(pkg) }
                .onFailure { Log.w(TAG, "addDisallowedApplication failed for $pkg", it) }
        }

        builder.setUnderlyingNetworks(null)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            builder.setBlocking(false)
        }

        return builder.establish()
            ?: throw IllegalStateException("establish() returned null — VPN consent revoked?")
    }

    private fun splitCidr(cidr: String): Pair<String, Int> {
        val parts = cidr.trim().split("/")
        val ip = parts[0]
        val defaultPrefix = if (ip.contains(':')) 128 else 32
        return ip to (parts.getOrNull(1)?.toIntOrNull() ?: defaultPrefix)
    }

    // -------------------------------------------------------------- lifecycle

    private suspend fun pollLoop(nodeName: String) {
        var lastState = ""
        while (currentCoroutineContext().isActive && running.get()) {
            delay(1000)
            try {
                VpnStateBus.applyJson(AegisEngine.statusJson())
                val state = VpnStateBus.status.value.state
                if (state != lastState) {
                    lastState = state
                    updateNotification("$nodeName · ${stateLabelOf(state).lowercase()}")
                }
            } catch (_: Exception) {
                // Engine busy or shutting down; keep polling.
            }
        }
    }

    private fun shutdown() {
        running.set(false)
        pollJob?.cancel()
        pollJob = null
        try {
            AegisEngine.stop()
        } catch (e: Exception) {
            Log.w(TAG, "aegislib.Stop failed", e)
        }
        tunFd?.let { runCatching { it.close() } }
        tunFd = null
        VpnStateBus.markDisconnected()
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
    }

    // ------------------------------------------------------------ notification

    private fun startAsForeground(text: String) {
        val notification = buildNotification(text)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIF_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIF_ID, notification)
        }
    }

    private fun buildNotification(text: String): Notification {
        val openApp = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val stop = PendingIntent.getService(
            this,
            1,
            disconnectIntent(this),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(getString(R.string.notif_title))
            .setContentText(text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setContentIntent(openApp)
            .addAction(0, getString(R.string.notif_disconnect), stop)
            .build()
    }

    private fun updateNotification(text: String) {
        val manager = getSystemService(NotificationManager::class.java) ?: return
        manager.notify(NOTIF_ID, buildNotification(text))
    }

    private fun stateLabelOf(state: String): String = when (state) {
        "connected" -> "Protected"
        "connecting" -> "Connecting"
        "disconnecting" -> "Disconnecting"
        else -> "Unprotected"
    }

    companion object {
        private const val TAG = "AegisVpnService"
        private const val NOTIF_ID = 4711
        private const val CHANNEL_ID = "aegis_vpn_tunnel"

        const val ACTION_DISCONNECT = "net.aegisvpn.action.DISCONNECT"
        const val EXTRA_PROFILE = "net.aegisvpn.extra.PROFILE"
        const val EXTRA_NODE = "net.aegisvpn.extra.NODE"
        const val EXTRA_OBFS = "net.aegisvpn.extra.OBFS"
        const val EXTRA_OBFS_KEY = "net.aegisvpn.extra.OBFS_KEY"
        const val EXTRA_THREAT = "net.aegisvpn.extra.THREAT"
        const val EXTRA_SPLIT_PACKAGES = "net.aegisvpn.extra.SPLIT_PACKAGES"

        fun ensureChannel(context: Context) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                val manager = context.getSystemService(NotificationManager::class.java) ?: return
                if (manager.getNotificationChannel(CHANNEL_ID) == null) {
                    val channel = NotificationChannel(
                        CHANNEL_ID,
                        context.getString(R.string.notif_channel),
                        NotificationManager.IMPORTANCE_LOW,
                    )
                    manager.createNotificationChannel(channel)
                }
            }
        }

        /** Start intent carrying profile + all option extras. */
        fun connectIntent(context: Context, store: ProfileStore): Intent =
            Intent(context, AegisVpnService::class.java).apply {
                putExtra(EXTRA_PROFILE, store.lastProfile.orEmpty())
                putExtra(EXTRA_NODE, store.selectedNode?.let { "${it.city}, ${it.country}" } ?: "AegisVPN")
                putExtra(EXTRA_OBFS, store.obfuscationEnabled)
                putExtra(EXTRA_OBFS_KEY, store.obfsKey)
                putExtra(EXTRA_THREAT, store.threatProtection)
                putExtra(EXTRA_SPLIT_PACKAGES, store.splitPackages.toTypedArray())
            }

        fun disconnectIntent(context: Context): Intent =
            Intent(context, AegisVpnService::class.java).setAction(ACTION_DISCONNECT)
    }
}

// ------------------------------------------------------------------ parser

/** Subset of wg-quick config relevant for building the TUN on this side. */
internal data class WgQuickConfig(
    val addresses: List<String>,
    val dnsServers: List<String>,
    val mtu: Int?,
    val allowedIps: List<String>,
)

/**
 * Small wg-quick parser (docs/PROTOCOL.md section 3). AegisObfs lines are left
 * untouched inside the text passed to aegislib; only routing parameters are
 * extracted here for VpnService.Builder.
 */
internal fun parseWgQuick(text: String): WgQuickConfig {
    var section = ""
    val addresses = mutableListOf<String>()
    val dns = mutableListOf<String>()
    val allowedIps = mutableListOf<String>()
    var mtu: Int? = null

    text.lineSequence().forEach { raw ->
        val line = raw.substringBefore('#').trim()
        if (line.isEmpty()) return@forEach
        if (line.startsWith("[")) {
            section = line.removePrefix("[").substringBefore(']').trim().lowercase()
            return@forEach
        }
        val eq = line.indexOf('=')
        if (eq <= 0) return@forEach
        val key = line.substring(0, eq).trim().lowercase()
        val value = line.substring(eq + 1).trim()
        when (section) {
            "interface" -> when (key) {
                "address" -> addresses += value.split(',')
                    .map { it.trim() }
                    .filter { it.isNotEmpty() }
                "dns" -> dns += value.split(',')
                    .map { it.trim() }
                    .filter { it.isNotEmpty() }
                "mtu" -> mtu = value.toIntOrNull()
            }
            "peer" -> if (key == "allowedips") {
                allowedIps += value.split(',')
                    .map { it.trim() }
                    .filter { it.isNotEmpty() }
            }
        }
    }
    return WgQuickConfig(addresses, dns, mtu, allowedIps)
}
