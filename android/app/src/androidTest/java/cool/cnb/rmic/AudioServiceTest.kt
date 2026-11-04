package cool.cnb.rmic

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import androidx.core.content.ContextCompat
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AudioServiceTest {
    private val instrumentation = InstrumentationRegistry.getInstrumentation()
    private val context: Context = instrumentation.targetContext

    @Test fun authorizationIsEncryptedAndTemporaryTokensAreNotPersisted() {
        val store = ProfileStore(context, "rmic-test-encryption")
        val token = "a".repeat(48)
        val profile = ConnectionProfile("https://192.168.1.10:8443", "sha256/" + "A".repeat(43) + "=", token)
        try {
            store.save(profile, true)
            assertEquals(profile, store.load())
            val stored = context.getSharedPreferences("rmic-test-encryption", Context.MODE_PRIVATE).all.toString()
            assertFalse(stored.contains(token))
            store.save(profile, false)
            assertEquals(profile.copy(token = ""), store.load())
        } finally { store.clear() }
    }

    // Requires an explicitly provided local integration host; never uses a user's remembered credentials.
    @Test fun threeModesSurviveScreenOffAndReleaseServerSessions() = runBlocking {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue("Provide origin, pin and token for the local integration host", args.containsKey("origin"))
        val profile = ConnectionProfile(args.getString("origin")!!, args.getString("pin")!!, args.getString("token")!!)
        assumeTrue(ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED)
        val store = ProfileStore(context)
        check(profile.origin !in store.origins()) { "Use a dedicated test server without an existing app profile" }
        val previousOrigin = store.selectedOrigin()
        val previousProfile = ClientRuntime.profile
        store.save(profile, true)
        ClientRuntime.profile = profile
        RmicApi(profile).use { api ->
            for (mode in AudioMode.entries) {
                ActivityScenario.launch(MainActivity::class.java).use { scenario ->
                    scenario.onActivity { activity ->
                        ContextCompat.startForegroundService(activity, Intent(activity, AudioService::class.java)
                            .setAction(AudioService.ACTION_START).putExtra("origin", profile.origin).putExtra("mode", mode.wire).putExtra("quality", "balanced"))
                    }
                    try {
                        try {
                            withTimeout(90_000) {
                                while (!ClientRuntime.audio.value.connected) {
                                    assertFalse(ClientRuntime.diagnostics(), ClientRuntime.audio.value.message.contains("失效"))
                                    delay(500)
                                }
                            }
                        } catch (timeout: kotlinx.coroutines.TimeoutCancellationException) {
                            throw AssertionError(ClientRuntime.diagnostics(), timeout)
                        }
                        shell("input keyevent KEYCODE_SLEEP")
                        delay(8000)
                        assertTrue(ClientRuntime.diagnostics(), ClientRuntime.audio.value.connected)
                        val status = api.request("/api/status")
                        assertEquals(mode.wire, status.getString("mode"))
                        if (mode.microphone) assertTrue(status.getJSONObject("receive").getJSONObject("audio").getLong("samples") > 0)
                        if (mode.playback) assertEquals("streaming", status.getJSONObject("send").getString("state"))
                        if (mode.microphone) {
                            context.startService(Intent(context, AudioService::class.java).setAction(AudioService.ACTION_MUTE))
                            withTimeout(5000) { while (!ClientRuntime.audio.value.muted) delay(100) }
                        }
                    } finally {
                        shell("input keyevent KEYCODE_WAKEUP")
                        shell("wm dismiss-keyguard")
                        context.startService(Intent(context, AudioService::class.java).setAction(AudioService.ACTION_STOP))
                        withTimeout(25_000) { while (ClientRuntime.audio.value.active) delay(100) }
                    }
                    assertEquals("waiting", api.status().state)
                }
            }
            ActivityScenario.launch(MainActivity::class.java).use { scenario ->
                scenario.onActivity { activity ->
                    ContextCompat.startForegroundService(activity, Intent(activity, AudioService::class.java)
                        .setAction(AudioService.ACTION_START).putExtra("origin", profile.origin).putExtra("mode", "mic").putExtra("quality", "balanced"))
                }
                withTimeout(10_000) { while (!ClientRuntime.audio.value.active) delay(100) }
                delay(300)
                context.startService(Intent(context, AudioService::class.java).setAction(AudioService.ACTION_STOP))
                withTimeout(25_000) { while (ClientRuntime.audio.value.active) delay(100) }
                assertEquals("waiting", api.status().state)
            }
        }
        store.remove(profile.origin)
        if (previousOrigin != null) store.select(previousOrigin)
        instrumentation.runOnMainSync {
            ClientRuntime.forgetProfile(profile.origin)
            ClientRuntime.profile = previousProfile
        }
    }

    private fun shell(command: String) {
        instrumentation.uiAutomation.executeShellCommand(command).close()
    }
}
