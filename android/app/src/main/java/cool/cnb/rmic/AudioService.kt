package cool.cnb.rmic

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ServiceInfo
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.withTimeoutOrNull
import org.json.JSONObject
import org.webrtc.PeerConnection
import javax.net.ssl.SSLException

object ClientRuntime {
    private val profiles = mutableMapOf<String, ConnectionProfile>()
    var profile: ConnectionProfile? = null
        set(value) {
            field = value
            if (value != null) profiles[value.origin] = value
        }
    fun cachedProfile(origin: String): ConnectionProfile? = profiles[origin]
    fun updateProfile(value: ConnectionProfile) {
        profiles[value.origin] = value
        if (profile?.origin == value.origin) profile = value
    }
    fun forgetProfile(origin: String) {
        profiles.remove(origin)
        if (profile?.origin == origin) profile = null
    }
    internal val mutableAudio = MutableStateFlow(AudioState())
    val audio = mutableAudio.asStateFlow()
    private val history = ArrayDeque<String>()
    fun record(message: String) {
        synchronized(history) {
            if (history.size >= 100) history.removeFirst()
            history.addLast("${java.time.Instant.now()} $message")
        }
    }
    fun diagnostics(): String = synchronized(history) {
        "RMic Android ${BuildConfig.VERSION_NAME}\nAndroid ${android.os.Build.VERSION.RELEASE} / ${android.os.Build.MANUFACTURER} ${android.os.Build.MODEL}\n" +
            history.joinToString("\n")
    }
}

class AudioService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var sessionJob: Job? = null
    private var engine: WebRtcEngine? = null
    private lateinit var audioManager: AudioManager
    private lateinit var wakeLock: PowerManager.WakeLock
    private var focusRequest: AudioFocusRequest? = null
    private var previousAudioMode = AudioManager.MODE_NORMAL
    private var changedAudioMode = false
    private var options = SessionOptions(AudioMode.MIC, "balanced")
    private var stopping = false
    private var destroyed = false
    private var recordingCallback: AudioManager.AudioRecordingCallback? = null
    private val noisyReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action == AudioManager.ACTION_AUDIO_BECOMING_NOISY) {
                stopSession("耳机已断开，请确认音频设备后重新开始")
            }
        }
    }

    override fun onCreate() {
        super.onCreate()
        audioManager = getSystemService(AudioManager::class.java)
        wakeLock = getSystemService(PowerManager::class.java)
            .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "RMic:AudioSession").apply { setReferenceCounted(false) }
        getSystemService(NotificationManager::class.java).createNotificationChannel(
            NotificationChannel(CHANNEL, "后台传音", NotificationManager.IMPORTANCE_LOW))
        ContextCompat.registerReceiver(this, noisyReceiver,
            IntentFilter(AudioManager.ACTION_AUDIO_BECOMING_NOISY), ContextCompat.RECEIVER_NOT_EXPORTED)
        if (android.os.Build.VERSION.SDK_INT >= 29) {
            val callback = object : AudioManager.AudioRecordingCallback() {
                override fun onRecordingConfigChanged(configs: MutableList<android.media.AudioRecordingConfiguration>) {
                    if (!stopping && options.mode.microphone && ClientRuntime.audio.value.active && configs.any { it.isClientSilenced }) {
                        stopSession("麦克风被系统或其他应用暂停，请处理后重新开始")
                    }
                }
            }
            recordingCallback = callback
            audioManager.registerAudioRecordingCallback(callback, Handler(Looper.getMainLooper()))
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> stopSession("已停止传音")
            ACTION_MUTE -> {
                if (!stopping && options.mode.microphone) {
                    val muted = !ClientRuntime.audio.value.muted
                    engine?.mute(muted)
                    ClientRuntime.mutableAudio.update { it.copy(muted = muted) }
                    updateNotification()
                }
            }
            ACTION_START -> {
                if (sessionJob == null) {
                    try {
                        stopping = false
                        options = SessionOptions(AudioMode.parse(intent.getStringExtra("mode").orEmpty()),
                            intent.getStringExtra("quality") ?: "balanced")
                        val type = if (android.os.Build.VERSION.SDK_INT >= 30) {
                            (if (options.mode.microphone) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE else 0) or
                                (if (options.mode.playback) ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK else 0)
                        } else if (android.os.Build.VERSION.SDK_INT >= 29 && options.mode.playback) {
                            ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK
                        } else 0
                        ClientRuntime.mutableAudio.value = AudioState(active = true, origin = intent.getStringExtra("origin").orEmpty(), mode = options.mode, message = "正在连接")
                        // Promote before rejecting a stale request; stopping an unpromoted FGS can crash the app.
                        ServiceCompat.startForeground(this, NOTIFICATION_ID, notification(), type)
                        val profile = checkNotNull(ClientRuntime.profile) { "请先配对电脑" }
                        require(profile.origin == intent.getStringExtra("origin")) { "所选服务器已变化，请重新开始传音" }
                        require(profile.token.isNotEmpty()) { "请先配对电脑" }
                        require(profile.pin.matches(Regex("sha256/[A-Za-z0-9+/]{43}="))) { "电脑证书记录无效，请清除本机记录后重新配对" }
                        prepareAudio()
                        wakeLock.acquire(10 * 60 * 1000L)
                        sessionJob = scope.launch { runSession(profile) }
                    } catch (error: Exception) {
                        stopSession(error.message ?: "无法启动后台音频")
                    }
                }
            }
            else -> if (sessionJob == null) stopSelf()
        }
        // A microphone foreground service must always be started by a visible user action.
        return START_NOT_STICKY
    }

    private fun prepareAudio() {
        if (options.mode.playback) {
            val attributes = AudioAttributes.Builder()
                .setUsage(if (options.mode == AudioMode.PLAYBACK) AudioAttributes.USAGE_MEDIA else AudioAttributes.USAGE_VOICE_COMMUNICATION)
                .setContentType(if (options.mode == AudioMode.PLAYBACK) AudioAttributes.CONTENT_TYPE_MUSIC else AudioAttributes.CONTENT_TYPE_SPEECH)
                .build()
            val request = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
                .setAudioAttributes(attributes).setAcceptsDelayedFocusGain(false)
                .setOnAudioFocusChangeListener({ change ->
                    if (change != AudioManager.AUDIOFOCUS_GAIN) stopSession("其他应用正在使用音频，请重新点击开始")
                }, Handler(Looper.getMainLooper())).build()
            check(audioManager.requestAudioFocus(request) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED) { "无法获得音频使用权" }
            focusRequest = request
        }
        if (options.mode.microphone) {
            previousAudioMode = audioManager.mode
            audioManager.mode = AudioManager.MODE_IN_COMMUNICATION
            changedAudioMode = true
        }
    }

    private suspend fun runSession(profile: ConnectionProfile) {
        val api = RmicApi(profile)
        var resumeID = ""
        var instance = ""
        var retries = 0
        try {
            while (currentCoroutineContext().isActive) {
                var id = ""
                var failure: Exception? = null
                try {
                    publish("正在连接电脑")
                    val status = api.status()
                    if (status.state == "waiting" && status.error == "电脑端已断开连接" && retries > 0) {
                        throw ApiException(409, "computer_disconnected", "电脑端已断开连接，请手动开始")
                    }
                    require(options.mode in status.modes) { "电脑未启用所选音频模式，请在电脑配置后重新开始" }
                    val quality = status.qualities.firstOrNull { it.id == options.quality }
                        ?: error("电脑不支持所选音质")
                    require(!quality.playbackOnly || options.mode == AudioMode.PLAYBACK) { "Ultra 仅用于收听电脑" }
                    if (instance != status.instance) resumeID = ""
                    instance = status.instance
                    val rtc = WebRtcEngine(applicationContext, options, quality)
                    engine = rtc
                    rtc.mute(ClientRuntime.audio.value.muted)
                    val offer = withTimeout(15_000) { rtc.offer() }
                    // Consume late answers even when Stop is pressed, so their server sessions can be deleted.
                    val result = withContext(NonCancellable) {
                        api.request("/api/session", "POST", JSONObject()
                            .put("type", offer.type.canonicalForm()).put("sdp", offer.description)
                            .put("mode", options.mode.wire).put("quality", options.quality)
                            .put("buffers", JSONObject().put("send_ms", 20).put("play_ms", 0)),
                            mapOf("X-RMic-Session" to resumeID, "X-RMic-Config" to status.revision.toString(),
                                "X-RMic-Instance" to status.instance)).also {
                            id = it.getString("id")
                            resumeID = id
                        }
                    }
                    currentCoroutineContext().ensureActive()
                    withTimeout(10_000) { rtc.answer(result.getJSONObject("answer")) }
                    monitor(api, rtc, status, id) { retries = 0 }
                } catch (timeout: kotlinx.coroutines.TimeoutCancellationException) {
                    failure = java.io.IOException("音频连接超时", timeout)
                } catch (cancelled: CancellationException) {
                    throw cancelled
                } catch (error: Exception) {
                    failure = error
                } finally {
                    ClientRuntime.mutableAudio.update { it.copy(connected = false, sessionId = "", shortcuts = null) }
                    engine?.close()
                    engine = null
                    withContext(NonCancellable) {
                        try { api.deleteSession(id) } catch (error: Exception) {
                            ClientRuntime.record("释放服务端会话：${error.javaClass.simpleName}")
                        }
                    }
                }
                val error = failure ?: break
                if (error is ApiException && error.terminal || error is SSLException || error is IllegalArgumentException || error is IllegalStateException) {
                    if (error is ApiException && error.status == 401) {
                        val cleared = profile.copy(token = "")
                        ClientRuntime.updateProfile(cleared)
                        withContext(Dispatchers.IO) { ProfileStore(this@AudioService).save(cleared, false, select = false) }
                    }
                    publish(error.message ?: "连接已停止")
                    break
                }
                val waitSeconds = minOf(1 shl minOf(retries++, 3), 8)
                publish("连接中断，${waitSeconds} 秒后重试（${error.javaClass.simpleName}）")
                delay(waitSeconds * 1000L)
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            publish(error.message ?: "传音失败")
        } finally {
            api.close()
            releaseAudio()
            sessionJob = null
            ClientRuntime.mutableAudio.update { it.copy(active = false, connected = false, stats = "") }
            if (!destroyed) {
                ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
                stopSelf()
            }
        }
    }

    private suspend fun monitor(api: RmicApi, rtc: WebRtcEngine, initial: ServerStatus, id: String, onConnected: () -> Unit) = coroutineScope {
        val connected = kotlinx.coroutines.CompletableDeferred<Unit>()
        launch {
            for (event in rtc.events) {
                when (event) {
                    PeerConnection.PeerConnectionState.CONNECTED -> {
                        connected.complete(Unit)
                        onConnected()
                        publish("${options.mode.label}中，可以锁屏使用", true)
                    }
                    PeerConnection.PeerConnectionState.DISCONNECTED,
                    PeerConnection.PeerConnectionState.FAILED,
                    PeerConnection.PeerConnectionState.CLOSED -> throw java.io.IOException("WebRTC 连接中断")
                    else -> Unit
                }
            }
        }
        launch { for (error in rtc.errors) throw error }
        withTimeout(25_000) { connected.await() }
        ClientRuntime.mutableAudio.update { it.copy(sessionId = id, shortcuts = initial.shortcuts) }
        while (isActive) {
            delay(2000)
            wakeLock.acquire(10 * 60 * 1000L)
            val status = api.status()
            if (status.state == "waiting" && status.error == "电脑端已断开连接") {
                throw ApiException(409, "computer_disconnected", "电脑端已断开连接，请手动开始")
            }
            if (status.instance != initial.instance || status.revision != initial.revision || status.state == "waiting") {
                throw java.io.IOException("电脑配置或会话已变化")
            }
            ClientRuntime.mutableAudio.update { it.copy(shortcuts = status.shortcuts) }
            if (status.directionError.isNotBlank()) {
                publish("音频方向异常：${status.directionError}", true)
            }
            val metrics = withTimeoutOrNull(2000) { rtc.metrics() }
            if (metrics != null) {
                api.request("/api/session/$id/metrics", "POST", metrics)
                ClientRuntime.mutableAudio.update { it.copy(stats =
                    "网络往返 ${metrics.optDouble("rtt_ms").toInt()} ms · 接收平均缓冲 ${metrics.optDouble("queued_ms").toInt()} ms") }
            }
        }
    }

    private fun publish(message: String, connected: Boolean = false) {
        if (ClientRuntime.audio.value.message == message && ClientRuntime.audio.value.connected == connected) return
        ClientRuntime.mutableAudio.update { it.copy(message = message, connected = connected, sessionId = if (connected) it.sessionId else "") }
        ClientRuntime.record(message)
        updateNotification()
    }

    private fun stopSession(message: String) {
        stopping = true
        engine?.mute(true)
        engine?.close()
        engine = null
        publish(message)
        releaseAudio()
        val job = sessionJob
        if (job != null) job.cancel() else {
            ClientRuntime.mutableAudio.update { it.copy(active = false, connected = false) }
            ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun releaseAudio() {
        if (wakeLock.isHeld) wakeLock.release()
        focusRequest?.let { audioManager.abandonAudioFocusRequest(it) }
        focusRequest = null
        if (changedAudioMode) {
            if (audioManager.mode == AudioManager.MODE_IN_COMMUNICATION) audioManager.mode = previousAudioMode
            changedAudioMode = false
        }
    }

    private fun notification(): Notification {
        val state = ClientRuntime.audio.value
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        fun action(name: String, code: Int) = PendingIntent.getService(this, code,
            Intent(this, AudioService::class.java).setAction(name), PendingIntent.FLAG_IMMUTABLE)
        return NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_mic).setContentTitle("RMic · ${options.mode.label}")
            .setContentText(if (state.muted) "麦克风已静音 · ${state.message}" else state.message)
            .setSubText(state.origin)
            .setContentIntent(open).setOngoing(true).setOnlyAlertOnce(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .apply {
                if (options.mode.microphone) addAction(0, if (state.muted) "取消静音" else "麦克风静音", action(ACTION_MUTE, 1))
                addAction(0, "停止", action(ACTION_STOP, 2))
            }.build()
    }

    private fun updateNotification() {
        if (!destroyed && ClientRuntime.audio.value.active) {
            getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification())
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null
    override fun onDestroy() {
        destroyed = true
        unregisterReceiver(noisyReceiver)
        recordingCallback?.let { audioManager.unregisterAudioRecordingCallback(it) }
        engine?.close()
        engine = null
        scope.cancel()
        releaseAudio()
        ClientRuntime.mutableAudio.update { it.copy(active = false, connected = false, sessionId = "", shortcuts = null) }
        super.onDestroy()
    }

    companion object {
        private const val CHANNEL = "rmic-audio"
        private const val NOTIFICATION_ID = 1
        const val ACTION_START = "cool.cnb.rmic.START"
        const val ACTION_STOP = "cool.cnb.rmic.STOP"
        const val ACTION_MUTE = "cool.cnb.rmic.MUTE"
    }
}
