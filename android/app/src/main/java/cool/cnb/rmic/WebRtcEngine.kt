package cool.cnb.rmic

import android.content.Context
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeout
import org.json.JSONObject
import org.webrtc.AudioSource
import org.webrtc.AudioTrack
import org.webrtc.DataChannel
import org.webrtc.IceCandidate
import org.webrtc.MediaConstraints
import org.webrtc.MediaStream
import org.webrtc.MediaStreamTrack
import org.webrtc.PeerConnection
import org.webrtc.PeerConnectionFactory
import org.webrtc.RtpReceiver
import org.webrtc.RtpTransceiver
import org.webrtc.SdpObserver
import org.webrtc.SessionDescription
import org.webrtc.audio.JavaAudioDeviceModule
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

class WebRtcEngine(context: Context, private val options: SessionOptions, quality: Quality) : AutoCloseable {
    val events = Channel<PeerConnection.PeerConnectionState>(Channel.CONFLATED)
    val errors = Channel<Throwable>(Channel.CONFLATED)
    private val gathered = CompletableDeferred<Unit>()
    private val module: JavaAudioDeviceModule
    private val factory: PeerConnectionFactory
    private val peer: PeerConnection
    private var source: AudioSource? = null
    private var microphone: AudioTrack? = null
    private val bitrate = quality.bitrate
    private var closed = false

    init {
        PeerConnectionFactory.initialize(PeerConnectionFactory.InitializationOptions.builder(context)
            .setEnableInternalTracer(false).createInitializationOptions())
        module = JavaAudioDeviceModule.builder(context)
            .setUseStereoInput(quality.channels == 2)
            .setUseStereoOutput(true)
            .setAudioAttributes(android.media.AudioAttributes.Builder()
                .setUsage(if (options.mode == AudioMode.PLAYBACK) android.media.AudioAttributes.USAGE_MEDIA
                    else android.media.AudioAttributes.USAGE_VOICE_COMMUNICATION)
                .setContentType(if (options.mode == AudioMode.PLAYBACK) android.media.AudioAttributes.CONTENT_TYPE_MUSIC
                    else android.media.AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .setAudioRecordErrorCallback(object : JavaAudioDeviceModule.AudioRecordErrorCallback {
                override fun onWebRtcAudioRecordInitError(message: String) { audioError(message) }
                override fun onWebRtcAudioRecordStartError(code: JavaAudioDeviceModule.AudioRecordStartErrorCode, message: String) { audioError(message) }
                override fun onWebRtcAudioRecordError(message: String) { audioError(message) }
            })
            .setAudioTrackErrorCallback(object : JavaAudioDeviceModule.AudioTrackErrorCallback {
                override fun onWebRtcAudioTrackInitError(message: String) { audioError(message) }
                override fun onWebRtcAudioTrackStartError(code: JavaAudioDeviceModule.AudioTrackStartErrorCode, message: String) { audioError(message) }
                override fun onWebRtcAudioTrackError(message: String) { audioError(message) }
            })
            .createAudioDeviceModule()
        factory = PeerConnectionFactory.builder().setAudioDeviceModule(module).createPeerConnectionFactory()
        val config = PeerConnection.RTCConfiguration(emptyList()).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
            continualGatheringPolicy = PeerConnection.ContinualGatheringPolicy.GATHER_ONCE
        }
        peer = checkNotNull(factory.createPeerConnection(config, object : PeerConnection.Observer {
            override fun onSignalingChange(state: PeerConnection.SignalingState) {}
            override fun onIceConnectionChange(state: PeerConnection.IceConnectionState) {}
            override fun onIceConnectionReceivingChange(receiving: Boolean) {}
            override fun onIceGatheringChange(state: PeerConnection.IceGatheringState) {
                if (state == PeerConnection.IceGatheringState.COMPLETE) gathered.complete(Unit)
            }
            override fun onIceCandidate(candidate: IceCandidate) {}
            override fun onIceCandidatesRemoved(candidates: Array<out IceCandidate>) {}
            override fun onAddStream(stream: MediaStream) {}
            override fun onRemoveStream(stream: MediaStream) {}
            override fun onDataChannel(channel: DataChannel) {}
            override fun onRenegotiationNeeded() {}
            override fun onAddTrack(receiver: RtpReceiver, streams: Array<out MediaStream>) {}
            override fun onConnectionChange(state: PeerConnection.PeerConnectionState) { events.trySend(state) }
            override fun onTrack(transceiver: RtpTransceiver) {
                // The receiver owns this track; it is released with the PeerConnection.
                (transceiver.receiver.track() as? AudioTrack)?.setEnabled(true)
            }
        })) { "无法创建 WebRTC 音频连接" }
        if (options.mode.microphone) {
            val constraints = MediaConstraints().apply {
                mandatory.add(MediaConstraints.KeyValuePair("googEchoCancellation", "true"))
                mandatory.add(MediaConstraints.KeyValuePair("googNoiseSuppression", "true"))
                mandatory.add(MediaConstraints.KeyValuePair("googAutoGainControl", "false"))
            }
            source = factory.createAudioSource(constraints)
            microphone = factory.createAudioTrack("phone-microphone", source)
            peer.addTransceiver(microphone, RtpTransceiver.RtpTransceiverInit(
                if (options.mode.playback) RtpTransceiver.RtpTransceiverDirection.SEND_RECV
                else RtpTransceiver.RtpTransceiverDirection.SEND_ONLY, listOf("rmic-phone")))
        } else {
            peer.addTransceiver(MediaStreamTrack.MediaType.MEDIA_TYPE_AUDIO,
                RtpTransceiver.RtpTransceiverInit(RtpTransceiver.RtpTransceiverDirection.RECV_ONLY))
        }
    }

    suspend fun offer(): SessionDescription {
        val offer = createOffer()
        setDescription(offer, local = true)
        withTimeout(10_000) { gathered.await() }
        return checkNotNull(peer.localDescription)
    }

    suspend fun answer(json: JSONObject) {
        setDescription(SessionDescription(SessionDescription.Type.fromCanonicalForm(json.getString("type")),
            json.getString("sdp")), local = false)
        peer.senders.filter { it.track() != null }.forEach { sender ->
            val parameters = sender.parameters
            parameters.encodings.forEach { it.maxBitrateBps = bitrate }
            check(sender.setParameters(parameters)) { "无法设置麦克风发送码率" }
        }
    }

    fun mute(muted: Boolean) { microphone?.setEnabled(!muted) }

    suspend fun metrics(): JSONObject = suspendCancellableCoroutine { continuation ->
        peer.getStats { report ->
            val values = report.statsMap.values
            val pair = values.firstOrNull {
                it.type == "candidate-pair" && it.members["state"] == "succeeded" && it.members["nominated"] == true
            }
            val inbound = values.firstOrNull { it.type == "inbound-rtp" && it.members["kind"] == "audio" }
            fun number(value: Any?) = (value as? Number)?.toDouble() ?: 0.0
            val emitted = number(inbound?.members?.get("jitterBufferEmittedCount"))
            val queued = if (emitted > 0) 1000 * number(inbound?.members?.get("jitterBufferDelay")) / emitted else 0.0
            val result = JSONObject().put("queued_ms", queued.coerceIn(0.0, 3000.0))
                .put("target_ms", 0).put("rtt_ms", (number(pair?.members?.get("currentRoundTripTime")) * 1000).coerceIn(0.0, 30000.0))
                .put("underruns", 0).put("received_frames", 0)
            if (continuation.isActive) continuation.resume(result)
        }
    }

    private suspend fun createOffer(): SessionDescription = suspendCancellableCoroutine { continuation ->
        peer.createOffer(object : SdpAdapter() {
            override fun onCreateSuccess(sdp: SessionDescription) { if (continuation.isActive) continuation.resume(sdp) }
            override fun onCreateFailure(message: String) {
                if (continuation.isActive) continuation.resumeWithException(IllegalStateException(message))
            }
        }, MediaConstraints())
    }

    private suspend fun setDescription(sdp: SessionDescription, local: Boolean): Unit = suspendCancellableCoroutine { continuation ->
        val observer = object : SdpAdapter() {
            override fun onSetSuccess() { if (continuation.isActive) continuation.resume(Unit) }
            override fun onSetFailure(message: String) {
                if (continuation.isActive) continuation.resumeWithException(IllegalStateException(message))
            }
        }
        if (local) peer.setLocalDescription(observer, sdp) else peer.setRemoteDescription(observer, sdp)
    }
    private fun audioError(message: String) {
        errors.trySend(ApiException(422, "audio_device_failed", "音频设备失效，请检查设备后重新开始：$message"))
    }

    override fun close() {
        if (closed) return
        closed = true
        peer.close()
        peer.dispose()
        microphone?.dispose()
        source?.dispose()
        factory.dispose()
        module.release()
        events.close()
        errors.close()
    }
}

private open class SdpAdapter : SdpObserver {
    override fun onCreateSuccess(sdp: SessionDescription) {}
    override fun onSetSuccess() {}
    override fun onCreateFailure(message: String) {}
    override fun onSetFailure(message: String) {}
}
