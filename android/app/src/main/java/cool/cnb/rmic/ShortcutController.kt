package cool.cnb.rmic

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject

// All state changes run on the owning (main) scope. Socket callbacks are marshalled
// onto it; stale callbacks can never restore a previous session's holds.
data class ShortcutState(
    val config: ShortcutConfig? = null,
    val ready: Boolean = false,
    val busy: Boolean = false,
    val heldButtons: Set<String> = emptySet(),
    val message: String = "连接音频后可使用",
)

class ShortcutController(private val scope: CoroutineScope) : AutoCloseable {
    private val mutableState = MutableStateFlow(ShortcutState())
    val state = mutableState.asStateFlow()
    private var profile: ConnectionProfile? = null
    private var session = ""
    private var foreground = false
    private var epoch = 0L
    private var sequence = 0L
    private var nextHold = 0L
    private var socket: WebSocket? = null
    private var api: RmicApi? = null
    private var connecting: Job? = null
    private var heartbeat: Job? = null
    private var readyDeadline: Job? = null
    private var retry: Job? = null
    private var lastReply = 0L
    private var runSequence: Long? = null
    private var closed = false
    private data class Hold(val id: String, val button: String, var acknowledged: Boolean = false)
    private val holds = mutableMapOf<String, Hold>()
    private val pending = mutableMapOf<Long, Hold>()

    fun update(profile: ConnectionProfile?, session: String, config: ShortcutConfig?) {
        if (closed) return
        val changed = this.profile != profile || this.session != session || mutableState.value.config?.revision != config?.revision
        if (changed) disconnect("连接音频后可使用")
        this.profile = profile
        this.session = session
        mutableState.value = mutableState.value.copy(config = config)
        connect()
    }

    fun foreground(value: Boolean) {
        if (closed || foreground == value) return
        foreground = value
        if (value) connect() else disconnect("已暂停快捷控制；后台传音不受影响")
    }

    private fun publishHolds() {
        mutableState.value = mutableState.value.copy(heldButtons = holds.values.filter { it.acknowledged }.map { it.button }.toSet())
    }

    private fun send(type: String, fields: JSONObject = JSONObject(), request: Hold? = null): Long? {
        if (!mutableState.value.ready) return null
        val seq = ++sequence
        fields.put("type", type).put("seq", seq).put("config_revision", mutableState.value.config?.revision)
        if (request != null) pending[seq] = request
        if (socket?.send(fields.toString()) != true) {
            failed("快捷控制发送失败，请重新操作")
            return null
        }
        return seq
    }

    fun begin(button: ShortcutButton, gesture: String): Boolean {
        val current = mutableState.value
        if (!current.ready || current.busy || button.mode != "hold" || button !in current.config?.buttons.orEmpty() || gesture in holds) return false
        val hold = Hold("$epoch-${++nextHold}", button.id)
        holds[gesture] = hold
        return send("button.hold_begin", JSONObject().put("button_id", button.id).put("hold_id", hold.id), hold) != null
    }

    fun end(gesture: String) {
        val hold = holds.remove(gesture) ?: return
        send("button.hold_end", JSONObject().put("hold_id", hold.id))
        publishHolds()
    }

    fun run(button: ShortcutButton) {
        val current = mutableState.value
        if (!current.ready || current.busy || holds.isNotEmpty() || button.mode != "tap" || button !in current.config?.buttons.orEmpty()) return
        mutableState.value = current.copy(busy = true, message = "执行中…")
        runSequence = send("button.run", JSONObject().put("button_id", button.id))
    }

    fun releaseAll() {
        send("release_all")
        runSequence = null
        holds.clear()
        pending.clear()
        mutableState.value = mutableState.value.copy(busy = false, heldButtons = emptySet(), message = "已请求停止并释放")
    }

    private fun connect() {
        val config = mutableState.value.config ?: return
        val profile = profile ?: return
        if (closed || !foreground || session.isEmpty() || socket != null || connecting != null || retry != null) return
        val attempt = epoch
        val controlApi = RmicApi(profile)
        api = controlApi
        mutableState.value = mutableState.value.copy(message = "正在连接快捷控制")
        connecting = scope.launch {
            try {
                val ticket = controlApi.shortcutTicket(session, config.revision)
                if (attempt != epoch) return@launch
                socket = controlApi.shortcutSocket(ticket, object : WebSocketListener() {
                    override fun onMessage(webSocket: WebSocket, text: String) {
                        scope.launch { if (attempt == epoch) receive(text) }
                    }
                    override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                        webSocket.close(code, null)
                        scope.launch { if (attempt == epoch) failed("快捷控制已断开，按键已停止") }
                    }
                    override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                        scope.launch { if (attempt == epoch) failed("快捷控制已断开，按键已停止") }
                    }
                    override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                        scope.launch { if (attempt == epoch) failed("快捷控制连接失败，请检查电脑与网络") }
                    }
                })
                readyDeadline = scope.launch {
                    delay(8000)
                    if (attempt == epoch && !mutableState.value.ready) failed("快捷控制连接超时")
                }
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                if (attempt == epoch) failed(error.message ?: "无法连接快捷控制")
            } finally {
                if (attempt == epoch) connecting = null
            }
        }
    }

    private fun receive(text: String) {
        try {
            val response = JSONObject(text)
            lastReply = System.nanoTime()
            when (response.getString("type")) {
                "ready" -> {
                    readyDeadline?.cancel(); readyDeadline = null
                    mutableState.value = mutableState.value.copy(ready = true, message = "作用于电脑当前窗口")
                    heartbeat?.cancel()
                    heartbeat = scope.launch {
                        while (true) {
                            if (System.nanoTime() - lastReply > 1_500_000_000L) {
                                failed("快捷控制响应超时，已停止操作")
                                break
                            }
                            send("heartbeat")
                            delay(250)
                        }
                    }
                }
                "ack" -> {
                    val request = pending.remove(response.getLong("seq"))
                    val error = response.optString("error")
                    if (error.isNotEmpty()) { failed(error); return }
                    request?.acknowledged = true
                    publishHolds()
                }
                "complete" -> {
                    if (response.getLong("seq") != runSequence) return
                    runSequence = null
                    mutableState.value = mutableState.value.copy(busy = false,
                        message = response.optString("error").ifBlank { "执行完成" })
                }
            }
        } catch (_: Exception) { failed("电脑返回的快捷控制消息无效") }
    }

    private fun failed(message: String) {
        disconnect(message)
        if (foreground && !closed && mutableState.value.config != null && session.isNotEmpty()) {
            retry = scope.launch { delay(2000); retry = null; connect() }
        }
    }

    private fun disconnect(message: String) {
        epoch++
        connecting?.cancel(); connecting = null
        heartbeat?.cancel(); heartbeat = null
        readyDeadline?.cancel(); readyDeadline = null
        retry?.cancel(); retry = null
        socket?.cancel(); socket = null
        api?.close(); api = null
        runSequence = null
        holds.clear(); pending.clear()
        mutableState.value = mutableState.value.copy(ready = false, busy = false, heldButtons = emptySet(), message = message)
    }

    override fun close() { closed = true; disconnect("快捷控制已关闭") }
}
