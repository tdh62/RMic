package cool.cnb.rmic

import android.app.Application
import android.content.Intent
import androidx.core.content.ContextCompat
import androidx.test.core.app.ActivityScenario
import androidx.lifecycle.ViewModelStore
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicBoolean

@RunWith(AndroidJUnit4::class)
class MultiServerTest {
    private val instrumentation = InstrumentationRegistry.getInstrumentation()
    private val application = instrumentation.targetContext.applicationContext as Application

    private class Host(val token: String, val instance: String, val mode: String, val quality: String) : AutoCloseable {
        val revoked = AtomicBoolean(false)
        val requests = ConcurrentLinkedQueue<RecordedRequest>()
        private val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        private val server = MockWebServer().apply {
            useHttps(HandshakeCertificates.Builder().heldCertificate(certificate).build().sslSocketFactory(), false)
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    requests.add(request)
                    if (request.getHeader("Authorization") != "Bearer $token" || revoked.get()) {
                        return MockResponse().setResponseCode(401).setHeader("X-RMic-Error", "unauthorized").setBody("授权已失效")
                    }
                    return when (request.path) {
                        "/api/status" -> MockResponse().setBody("""{
                            "instance_id":"$instance", "config_revision":1, "modes":["$mode"],
                            "qualities":[{"id":"$quality","bitrate":64000,"channels":1}],
                            "state":"waiting", "default_mode":"$mode", "default_quality":{"id":"$quality"}
                        }""")
                        "/api/pair/forget" -> MockResponse().setBody("{}")
                        else -> MockResponse().setResponseCode(404)
                    }
                }
            }
            start()
        }
        val profile = ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(certificate.certificate), token)
        override fun close() { server.close() }
    }

    @Test fun switchingAndRevokingServersKeepsCredentialsAndCapabilitiesSeparate() = runBlocking {
        val store = ProfileStore(application, "rmic-test-multi-server-ui")
        store.clear()
        val viewModels = ViewModelStore()
        var previous: ConnectionProfile? = null
        instrumentation.runOnMainSync {
            previous = ClientRuntime.profile
            ClientRuntime.profile = null
            ClientRuntime.mutableAudio.value = AudioState()
        }
        try {
            Host("a".repeat(48), "computer-a", "mic", "balanced").use { first ->
                Host("b".repeat(48), "computer-b", "playback", "music").use { second ->
                    store.save(first.profile, true)
                    store.save(second.profile, true, select = false)
                    lateinit var model: ClientViewModel
                    instrumentation.runOnMainSync {
                        model = ClientViewModel(application, store)
                        viewModels.put("multi-server", model)
                    }
                    suspend fun settled() = withTimeout(30_000) { while (model.ui.value.busy) delay(50) }
                    settled()
                    assertEquals("computer-a", model.ui.value.status?.instance)
                    assertEquals(first.profile, ClientRuntime.profile)

                    instrumentation.runOnMainSync { model.selectServer(second.profile.origin) }
                    settled()
                    assertEquals("computer-b", model.ui.value.status?.instance)
                    assertEquals(AudioMode.PLAYBACK, model.options().mode)
                    assertEquals("music", model.options().quality)
                    assertEquals(first.profile, store.load(first.profile.origin))

                    first.revoked.set(true)
                    instrumentation.runOnMainSync { model.selectServer(first.profile.origin) }
                    settled()
                    assertFalse(model.ui.value.authorized)
                    assertEquals("", store.load(first.profile.origin)!!.token)
                    assertEquals(second.profile, store.load(second.profile.origin))

                    instrumentation.runOnMainSync { model.selectServer(second.profile.origin) }
                    settled()
                    assertEquals("computer-b", model.ui.value.status?.instance)
                    instrumentation.runOnMainSync {
                        ClientRuntime.mutableAudio.value = AudioState(active = true, connected = true, origin = second.profile.origin)
                        model.selectServer(first.profile.origin)
                        model.addServer()
                    }
                    settled()
                    assertEquals(second.profile.origin, model.ui.value.selectedOrigin)
                    assertEquals(second.profile, ClientRuntime.profile)
                    assertTrue(model.ui.value.error.contains("先停止"))
                    instrumentation.runOnMainSync { ClientRuntime.mutableAudio.value = AudioState() }
                    val requestsBeforeStaleStart = second.requests.size
                    ActivityScenario.launch(MainActivity::class.java).use { scenario ->
                        scenario.onActivity { activity ->
                            ContextCompat.startForegroundService(activity, Intent(activity, AudioService::class.java)
                                .setAction(AudioService.ACTION_START).putExtra("origin", first.profile.origin)
                                .putExtra("mode", "playback").putExtra("quality", "music"))
                        }
                        withTimeout(15_000) {
                            while (ClientRuntime.audio.value.active || !ClientRuntime.audio.value.message.contains("服务器已变化")) delay(50)
                        }
                        assertFalse(ClientRuntime.audio.value.active)
                        assertEquals(second.profile, ClientRuntime.profile)
                        assertEquals(requestsBeforeStaleStart, second.requests.size)
                    }
                    instrumentation.runOnMainSync {
                        ClientRuntime.mutableAudio.value = AudioState()
                        model.forget()
                    }
                    settled()
                    assertNull(store.load(second.profile.origin))
                    assertEquals(listOf(first.profile.origin), store.origins())
                    assertEquals("", store.load(first.profile.origin)!!.token)
                    assertTrue(first.requests.all { it.getHeader("Authorization") == "Bearer ${first.token}" && it.path == "/api/status" })
                    assertTrue(second.requests.all { it.getHeader("Authorization") == "Bearer ${second.token}" })
                    assertTrue(second.requests.any { it.path == "/api/pair/forget" })

                    instrumentation.runOnMainSync { model.selectServer(first.profile.origin) }
                    settled()
                    instrumentation.runOnMainSync { model.clearLocal() }
                    settled()
                    assertTrue(store.origins().isEmpty())
                    instrumentation.runOnMainSync {
                        ClientRuntime.forgetProfile(first.profile.origin)
                        ClientRuntime.forgetProfile(second.profile.origin)
                    }
                }
            }
        } finally {
            instrumentation.runOnMainSync {
                viewModels.clear()
                ClientRuntime.mutableAudio.value = AudioState()
                ClientRuntime.profile = previous
            }
            store.clear()
        }
    }
}
