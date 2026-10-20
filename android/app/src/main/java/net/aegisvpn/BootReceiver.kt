package net.aegisvpn

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log

/**
 * Auto-connect on boot: when enabled in Settings, starts AegisVpnService with
 * the last stored profile (foreground-service start from BOOT_COMPLETED is an
 * allowed exemption). VPN consent, once granted, survives reboots, so no user
 * interaction is required here.
 */
class BootReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return

        val result = goAsync() // EncryptedSharedPreferences does disk IO; get off the main thread
        Thread {
            try {
                val store = ProfileStore(context)
                if (!store.autoConnect) return@Thread
                if (store.lastProfile.isNullOrBlank()) return@Thread

                val service = AegisVpnService.connectIntent(context, store)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    context.startForegroundService(service)
                } else {
                    context.startService(service)
                }
                Log.i(TAG, "Auto-connect: tunnel service started on boot")
            } catch (e: Exception) {
                Log.w(TAG, "Auto-connect on boot failed", e)
            } finally {
                result.finish()
            }
        }.start()
    }

    private companion object {
        const val TAG = "BootReceiver"
    }
}
