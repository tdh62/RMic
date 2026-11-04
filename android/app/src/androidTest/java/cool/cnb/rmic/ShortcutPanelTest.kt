package cool.cnb.rmic

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.launch
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

@RunWith(AndroidJUnit4::class)
class ShortcutPanelTest {
    @get:Rule val compose = createComposeRule()

    @Test fun hiddenNarrowLayoutHoldCancelAndBackgroundRelease() {
        val certificate = HeldCertificate.Builder().addSubjectAlternativeName("localhost").build()
        MockWebServer().use { server ->
            server.useHttps(HandshakeCertificates.Builder().heldCertificate(certificate).build().sslSocketFactory(), false)
            server.start()
            val messages = LinkedBlockingQueue<JSONObject>()
            server.enqueue(MockResponse().setBody("{\"ticket\":\"once\"}"))
            server.enqueue(MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) { webSocket.send("{\"type\":\"ready\"}") }
                override fun onMessage(webSocket: WebSocket, text: String) {
                    val message = JSONObject(text)
                    messages.offer(message)
                    webSocket.send(JSONObject().put("type", "ack").put("seq", message.getLong("seq")).toString())
                }
            }))
            val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
            val controller = ShortcutController(scope)
            var state by mutableStateOf(ShortcutState())
            compose.setContent {
                MaterialTheme { Box(Modifier.width(280.dp)) { ShortcutPanel(state, controller) } }
            }
            compose.onNodeWithText("电脑快捷按钮").assertDoesNotExist()
            val profile = ConnectionProfile(server.url("/").toString().removeSuffix("/"), ServerTrust.pin(certificate.certificate), "a".repeat(48))
            val config = ShortcutConfig(3, listOf(
                ShortcutButton("1", "按住说话", "hold"), ShortcutButton("2", "固定回复", "tap"), ShortcutButton("3", "另一组合", "hold")))
            val collect = scope.launch { controller.state.collect { state = it } }
            try {
                compose.runOnIdle { controller.update(profile, "audio", config); controller.foreground(true) }
                compose.waitUntil(10_000) { controller.state.value.ready }
                compose.onNodeWithText("按住说话").assertIsDisplayed()
                compose.onNodeWithText("固定回复").assertIsDisplayed()
                compose.onNodeWithText("另一组合").assertIsDisplayed()
                compose.onNodeWithText("按住说话").performTouchInput { down(center) }
                awaitMessage(messages, "button.hold_begin")
                compose.waitUntil(5000) { controller.state.value.heldButtons == setOf("1") }
                compose.onNodeWithText("固定回复").assertIsNotEnabled()
                compose.onNodeWithText("按住说话").performTouchInput { cancel() }
                awaitMessage(messages, "button.hold_end")
                compose.runOnIdle { assertTrue(controller.state.value.heldButtons.isEmpty()) }
                compose.onNodeWithText("按住说话").performTouchInput { down(center) }
                awaitMessage(messages, "button.hold_begin")
                compose.runOnIdle { controller.foreground(false) }
                compose.onNodeWithText("按住说话").assertIsNotEnabled()
                compose.runOnIdle {
                    assertFalse(controller.state.value.ready)
                    assertTrue(controller.state.value.heldButtons.isEmpty())
                    controller.update(profile, "audio", null)
                }
                compose.onNodeWithText("电脑快捷按钮").assertDoesNotExist()
            } finally {
                compose.runOnIdle { controller.close() }
                collect.cancel()
                scope.cancel()
            }
        }
    }

    private fun awaitMessage(messages: LinkedBlockingQueue<JSONObject>, type: String): JSONObject {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5)
        while (System.nanoTime() < deadline) {
            val message = messages.poll(100, TimeUnit.MILLISECONDS) ?: continue
            if (message.getString("type") == type) return message
        }
        throw AssertionError("Missing $type")
    }
}
