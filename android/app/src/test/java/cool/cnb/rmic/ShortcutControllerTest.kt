package cool.cnb.rmic

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.TimeUnit

class ShortcutControllerTest {
    private class Host : AutoCloseable {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        val server = MockWebServer().apply {
            useHttps(HandshakeCertificates.Builder().heldCertificate(certificate).build().sslSocketFactory(), false)
            start()
        }
        val profile = ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(certificate.certificate), "a".repeat(48))
        val messages = Channel<JSONObject>(Channel.UNLIMITED)
        var socket: WebSocket? = null
        @Volatile var rejectHold = false
        @Volatile var acknowledge = true
        fun enqueueControl() {
            server.enqueue(MockResponse().setBody("{\"ticket\":\"one-time-ticket\"}"))
            server.enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) {
                    socket = webSocket
                    webSocket.send("{\"type\":\"ready\"}")
                }
                override fun onMessage(webSocket: WebSocket, text: String) {
                    val message = JSONObject(text)
                    messages.trySend(message)
                    if (acknowledge) {
                        val reply = JSONObject().put("type", "ack").put("seq", message.getLong("seq"))
                        if (rejectHold && message.getString("type") == "button.hold_begin") reply.put("error", "按键注入失败")
                        webSocket.send(reply.toString())
                    }
                }
            }))
        }
        suspend fun message(type: String): JSONObject = withTimeout(5000) {
            while (true) {
                val message = messages.receive()
                if (message.getString("type") == type) return@withTimeout message
            }
            @Suppress("UNREACHABLE_CODE") error("unreachable")
        }
        override fun close() { socket?.close(1000, null); messages.close(); server.close() }
    }
    private val hold = ShortcutButton("1", "按住说话", "hold")
    private val tap = ShortcutButton("2", "固定回复", "tap")
    private val config = ShortcutConfig(7, listOf(hold, tap))
    private suspend fun await(check: () -> Boolean) = withTimeout(5000) { while (!check()) delay(10) }
    private suspend fun connect(scope: CoroutineScope, host: Host): ShortcutController {
        host.enqueueControl()
        val controller = ShortcutController(scope)
        controller.update(host.profile, "session-1", config)
        controller.foreground(true)
        await { controller.state.value.ready }
        return controller
    }

    @Test fun ticketAndSocketUsePinnedOriginWithoutLeakingLongTermToken() = runBlocking {
        Host().use { host ->
            val controller = connect(this, host)
            try {
                val ticket = host.server.takeRequest(2, TimeUnit.SECONDS)!!
                assertEquals("/api/shortcuts/ticket", ticket.path)
                assertEquals("Bearer ${host.profile.token}", ticket.getHeader("Authorization"))
                assertEquals("session-1", ticket.getHeader("X-RMic-Session"))
                assertEquals("7", ticket.getHeader("X-RMic-Shortcuts"))
                val socket = host.server.takeRequest(2, TimeUnit.SECONDS)!!
                assertEquals("/api/shortcuts/control?ticket=one-time-ticket", socket.path)
                assertEquals(host.profile.origin, socket.getHeader("Origin"))
                assertNull(socket.getHeader("Authorization"))
                assertFalse(socket.path!!.contains(host.profile.token))
            } finally { controller.close() }
        }
    }

    @Test fun holdsReleaseAndSequencesDoNotOverlapOrReplay() = runBlocking {
        Host().use { host ->
            val controller = connect(this, host)
            try {
                assertTrue(controller.begin(hold, "finger"))
                val begin = host.message("button.hold_begin")
                assertEquals("1", begin.getString("button_id"))
                assertEquals(7, begin.getInt("config_revision"))
                assertFalse(begin.has("keys"))
                await { "1" in controller.state.value.heldButtons }
                controller.run(tap)
                assertFalse(controller.state.value.busy)
                controller.end("finger")
                assertEquals(begin.getString("hold_id"), host.message("button.hold_end").getString("hold_id"))
                assertTrue(controller.state.value.heldButtons.isEmpty())
                controller.run(tap)
                val run = host.message("button.run")
                assertTrue(controller.state.value.busy)
                assertFalse(controller.begin(hold, "blocked"))
                controller.releaseAll()
                host.message("release_all")
                controller.run(tap)
                val nextRun = host.message("button.run")
                host.socket!!.send(JSONObject().put("type", "complete").put("seq", run.getLong("seq")).toString())
                delay(50)
                assertTrue("Old completion must not finish a new run", controller.state.value.busy)
                host.socket!!.send(JSONObject().put("type", "complete").put("seq", nextRun.getLong("seq")).toString())
                await { !controller.state.value.busy }
                assertTrue(controller.begin(hold, "finger-2"))
                host.message("button.hold_begin")
                controller.foreground(false)
                assertFalse(controller.state.value.ready)
                assertTrue(controller.state.value.heldButtons.isEmpty())
                host.enqueueControl()
                controller.foreground(true)
                await { controller.state.value.ready }
                assertTrue(controller.state.value.heldButtons.isEmpty())
                delay(100)
                val remaining = mutableListOf<String>()
                while (true) { val m = host.messages.tryReceive().getOrNull() ?: break; remaining += m.getString("type") }
                assertFalse(remaining.contains("button.hold_begin"))
                assertFalse(remaining.contains("button.run"))
            } finally { controller.close() }
        }
    }

    @Test fun disabledConfigurationClosesControlAndHidesButtons() = runBlocking {
        Host().use { host ->
            val controller = connect(this, host)
            try {
                controller.begin(hold, "finger")
                host.message("button.hold_begin")
                controller.update(host.profile, "session-1", null)
                assertNull(controller.state.value.config)
                assertFalse(controller.state.value.ready)
                assertTrue(controller.state.value.heldButtons.isEmpty())
                assertFalse(controller.begin(hold, "new"))
            } finally { controller.close() }
        }
    }

    @Test fun rejectionAndMissingHeartbeatAcksClearLocalHolds() = runBlocking {
        Host().use { host ->
            val controller = connect(this, host)
            try {
                host.rejectHold = true
                controller.begin(hold, "finger")
                await { controller.state.value.message == "按键注入失败" }
                assertFalse(controller.state.value.ready)
                assertTrue(controller.state.value.heldButtons.isEmpty())
                controller.foreground(false)
                host.rejectHold = false
                host.enqueueControl()
                controller.foreground(true)
                await { controller.state.value.ready }
                controller.begin(hold, "finger-2")
                await { controller.state.value.heldButtons.isNotEmpty() }
                host.acknowledge = false
                await { !controller.state.value.ready }
                assertTrue(controller.state.value.heldButtons.isEmpty())
                assertTrue(controller.state.value.message.contains("超时"))
            } finally { controller.close() }
        }
    }
    @Test fun configurationAndServerChangesInvalidatePendingHolds() = runBlocking {
        Host().use { host ->
            val controller = connect(this, host)
            try {
                controller.begin(hold, "old-finger")
                val oldBegin = host.message("button.hold_begin")
                host.enqueueControl()
                controller.update(host.profile, "session-2", config.copy(revision = 8))
                assertFalse(controller.state.value.ready)
                assertTrue(controller.state.value.heldButtons.isEmpty())
                await { controller.state.value.ready }
                controller.end("old-finger")
                assertTrue(controller.begin(hold, "new-finger"))
                val newBegin = host.message("button.hold_begin")
                assertEquals(8, newBegin.getInt("config_revision"))
                assertNotEquals(oldBegin.getString("hold_id"), newBegin.getString("hold_id"))
                controller.update(null, "", null)
                assertFalse(controller.state.value.ready)
                assertNull(controller.state.value.config)
                assertTrue(controller.state.value.heldButtons.isEmpty())
            } finally { controller.close() }
        }
    }
}
