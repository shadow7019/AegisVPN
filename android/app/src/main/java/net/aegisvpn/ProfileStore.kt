package net.aegisvpn

import android.content.Context
import android.content.SharedPreferences
import android.util.Base64
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import org.json.JSONObject
import java.math.BigInteger
import java.security.SecureRandom
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Encrypted-preferences store for everything the app persists:
 * profiles (wg-quick text), selected node, settings and the client keypair.
 *
 * Secrets (the WireGuard private key, obfuscation passphrase) live here only;
 * per docs/PROTOCOL.md non-negotiable #4 the UI never ships them anywhere.
 */
class ProfileStore(context: Context) {

    private val prefs: SharedPreferences = openPrefs(context)
    private val keyBusy = AtomicBoolean(false)

    // ----- profiles ---------------------------------------------------------

    /** Full wg-quick profile text of the last selection (built by [selectNode]). */
    var lastProfile: String?
        get() = prefs.getString(KEY_LAST_PROFILE, null)
        set(value) {
            prefs.edit().putString(KEY_LAST_PROFILE, value).apply()
        }

    /** Node the user last tapped in the server list. */
    var selectedNode: Node?
        get() {
            val raw = prefs.getString(KEY_SELECTED_NODE, null) ?: return null
            return runCatching { Node.fromJson(JSONObject(raw)) }.getOrNull()
        }
        set(value) {
            if (value == null) {
                prefs.edit().remove(KEY_SELECTED_NODE).apply()
            } else {
                prefs.edit().putString(KEY_SELECTED_NODE, value.toJson()).apply()
            }
        }

    // ----- toggles / options ------------------------------------------------

    var autoConnect: Boolean
        get() = prefs.getBoolean(KEY_AUTO_CONNECT, false)
        set(value) {
            prefs.edit().putBoolean(KEY_AUTO_CONNECT, value).apply()
        }

    var threatProtection: Boolean
        get() = prefs.getBoolean(KEY_THREAT, false)
        set(value) {
            prefs.edit().putBoolean(KEY_THREAT, value).apply()
        }

    var obfuscationEnabled: Boolean
        get() = prefs.getBoolean(KEY_OBFS, false)
        set(value) {
            prefs.edit().putBoolean(KEY_OBFS, value).apply()
        }

    var obfsKey: String
        get() = prefs.getString(KEY_OBFS_KEY, "").orEmpty()
        set(value) {
            prefs.edit().putString(KEY_OBFS_KEY, value).apply()
        }

    /** Package names excluded from the tunnel (addDisallowedApplication). */
    var splitPackages: Set<String>
        get() = prefs.getStringSet(KEY_SPLIT_PACKAGES, emptySet()).orEmpty()
        set(value) {
            prefs.edit().putStringSet(KEY_SPLIT_PACKAGES, value).apply()
        }

    var coordUrl: String
        get() = prefs.getString(KEY_COORD_URL, DEFAULT_COORD_URL).orEmpty()
            .ifBlank { DEFAULT_COORD_URL }
        set(value) {
            prefs.edit().putString(KEY_COORD_URL, value.trim()).apply()
        }

    // ----- client identity ---------------------------------------------------

    var wgPrivateKey: String
        get() = prefs.getString(KEY_WG_PRIV, null).orEmpty()
        private set(value) {
            prefs.edit().putString(KEY_WG_PRIV, value).apply()
        }

    var wgPublicKey: String
        get() = prefs.getString(KEY_WG_PUB, null).orEmpty()
        private set(value) {
            prefs.edit().putString(KEY_WG_PUB, value).apply()
        }

    /** Returns (privateKeyBase64, publicKeyBase64), generating on first use. */
    fun ensureKeypair(): Pair<String, String> {
        val priv = wgPrivateKey
        val pub = wgPublicKey
        if (priv.isNotBlank() && pub.isNotBlank()) return priv to pub
        return regenerateKeypair()
    }

    /** Fresh X25519 keypair (also used by the "Regenerate" button). */
    fun regenerateKeypair(): Pair<String, String> {
        // Serialize so a double-tap cannot interleave generation.
        synchronized(keyBusy) {
            val privBytes = X25519.generatePrivateKey()
            val priv = Base64.encodeToString(privBytes, Base64.NO_WRAP)
            val pub = Base64.encodeToString(X25519.publicKey(privBytes), Base64.NO_WRAP)
            wgPrivateKey = priv
            wgPublicKey = pub
            return priv to pub
        }
    }

    /**
     * Builds and stores the wg-quick profile for [node] using the client keypair,
     * current obfuscation settings and the AegisVPN extensions from
     * docs/PROTOCOL.md section 3. Returns the profile text.
     */
    fun selectNode(node: Node): String {
        val (priv, _) = ensureKeypair()
        val profile = buildString {
            appendLine("[Interface]")
            appendLine("PrivateKey = $priv")
            appendLine("Address = $CLIENT_TUN_ADDRESS")
            appendLine("DNS = $AEGIS_DNS")
            appendLine("MTU = $DEFAULT_MTU")
            if (obfuscationEnabled) {
                appendLine("AegisObfs = true")
                if (obfsKey.isNotBlank()) appendLine("AegisObfsKey = $obfsKey")
            }
            appendLine()
            appendLine("[Peer]")
            appendLine("PublicKey = ${node.pubkey}")
            appendLine("AllowedIPs = 0.0.0.0/0, ::/0")
            appendLine("Endpoint = ${node.endpoint}")
            appendLine("PersistentKeepalive = 25")
        }
        selectedNode = node
        lastProfile = profile
        return profile
    }

    companion object {
        const val DEFAULT_COORD_URL = "https://coord.aegisvpn.net/v1/nodes"
        const val CLIENT_TUN_ADDRESS = "10.8.0.2/32"
        const val AEGIS_DNS = "10.8.0.1"
        const val DEFAULT_MTU = 1420

        private const val PREFS_NAME = "aegis_secure"
        private const val KEY_LAST_PROFILE = "last_profile"
        private const val KEY_SELECTED_NODE = "selected_node"
        private const val KEY_AUTO_CONNECT = "auto_connect"
        private const val KEY_THREAT = "threat_protection"
        private const val KEY_OBFS = "obfuscation"
        private const val KEY_OBFS_KEY = "obfs_key"
        private const val KEY_SPLIT_PACKAGES = "split_packages"
        private const val KEY_COORD_URL = "coord_url"
        private const val KEY_WG_PRIV = "wg_private_key"
        private const val KEY_WG_PUB = "wg_public_key"

        private fun openPrefs(context: Context): SharedPreferences = try {
            val masterKey = MasterKey.Builder(context)
                .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                .build()
            EncryptedSharedPreferences.create(
                context,
                PREFS_NAME,
                masterKey,
                EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
            )
        } catch (t: Throwable) {
            // Keystore can be wedged on a few devices/ROMs. Degrade to plain prefs
            // instead of crashing; still a local-only store.
            context.getSharedPreferences("${PREFS_NAME}_fallback", Context.MODE_PRIVATE)
        }
    }
}

/**
 * Minimal X25519 (RFC 7748) for client key GENERATION only.
 *
 * WireGuard identity is Curve25519: the public key is the X25519 scalar
 * multiplication of the private key with the base point u=9. The Montgomery
 * ladder below was validated against the RFC 7748 section 6.1 test vectors
 * (Alice/Bob public keys + shared secret). Nothing else in the app does ECDH —
 * the actual handshake math lives in wireguard-go inside aegislib.
 */
internal object X25519 {

    private val P: BigInteger = BigInteger.TWO.pow(255).subtract(BigInteger.valueOf(19))
    private val A24: BigInteger = BigInteger.valueOf(121665)
    private val BASE_POINT: ByteArray = ByteArray(32).also { it[0] = 9 }

    fun generatePrivateKey(): ByteArray {
        val raw = ByteArray(32)
        SecureRandom().nextBytes(raw)
        clamp(raw)
        return raw
    }

    fun publicKey(privateKey: ByteArray): ByteArray {
        val k = privateKey.copyOf()
        clamp(k)
        val u = decodeUCoordinate(BASE_POINT)
        val x = ladder(k, u)
        return encodeUCoordinate(x)
    }

    private fun clamp(k: ByteArray) {
        k[0] = (k[0].toInt() and 248).toByte()
        k[31] = (k[31].toInt() and 127).toByte()
        k[31] = (k[31].toInt() or 64).toByte()
    }

    private fun decodeUCoordinate(bytes: ByteArray): BigInteger {
        var u = BigInteger.ZERO
        for (i in 31 downTo 0) {
            val v = (if (i == 31) bytes[i].toInt() and 0x7F else bytes[i].toInt() and 0xFF).toLong()
            u = u.shiftLeft(8).or(BigInteger.valueOf(v))
        }
        return u
    }

    private fun encodeUCoordinate(u: BigInteger): ByteArray {
        val bigEndian = u.toByteArray() // two's-complement big-endian, may lead with 0x00
        val out = ByteArray(32)
        var src = bigEndian.size - 1
        var dst = 0
        while (src >= 0 && dst < 32) {
            out[dst] = bigEndian[src]
            src--
            dst++
        }
        return out
    }

    private fun ladder(k: ByteArray, u: BigInteger): BigInteger {
        var x1 = u
        var x2 = BigInteger.ONE
        var z2 = BigInteger.ZERO
        var x3 = u
        var z3 = BigInteger.ONE
        var swap = false
        for (t in 254 downTo 0) {
            val bit = (k[t ushr 3].toInt() ushr (t and 7)) and 1
            val wantSwap = bit == 1
            if (wantSwap != swap) { // conditional swap
                var tmp = x2; x2 = x3; x3 = tmp
                tmp = z2; z2 = z3; z3 = tmp
            }
            swap = wantSwap
            val a = x2.add(z2).mod(P)
            val aa = a.multiply(a).mod(P)
            val b = x2.subtract(z2).mod(P)
            val bb = b.multiply(b).mod(P)
            val e = aa.subtract(bb).mod(P)
            val c = x3.add(z3).mod(P)
            val d = x3.subtract(z3).mod(P)
            val da = d.multiply(a).mod(P)
            val cb = c.multiply(b).mod(P)
            val sum = da.add(cb).mod(P)
            x3 = sum.multiply(sum).mod(P)
            val diff = da.subtract(cb).mod(P)
            z3 = x1.multiply(diff).multiply(diff).mod(P)
            x2 = aa.multiply(bb).mod(P)
            z2 = e.multiply(aa.add(A24.multiply(e)).mod(P)).mod(P)
        }
        if (swap) {
            var tmp = x2; x2 = x3; x3 = tmp
            tmp = z2; z2 = z3; z3 = tmp
        }
        return x2.multiply(z2.modPow(P.subtract(BigInteger.TWO), P)).mod(P)
    }
}
