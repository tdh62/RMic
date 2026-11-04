package cool.cnb.rmic

import okhttp3.HttpUrl.Companion.toHttpUrl
import org.json.JSONObject

enum class AudioMode(val wire: String, val label: String, val microphone: Boolean, val playback: Boolean) {
    MIC("mic", "手机麦克风", true, false),
    PLAYBACK("playback", "收听电脑", false, true),
    DUPLEX("duplex", "双向传音", true, true);

    companion object {
        fun parse(value: String) = entries.firstOrNull { it.wire == value }
            ?: error("不支持的音频模式：$value")
    }
}

data class Quality(val id: String, val bitrate: Int, val channels: Int, val playbackOnly: Boolean) {
    companion object {
        fun fromJson(json: JSONObject) = Quality(
            json.getString("id"), json.getInt("bitrate"), json.getInt("channels"),
            json.optBoolean("playback_only")
        )
    }
}
data class ShortcutButton(val id: String, val name: String, val mode: String)
data class ShortcutConfig(val revision: Long, val buttons: List<ShortcutButton>, val error: String = "") {
    companion object {
        fun fromJson(json: JSONObject): ShortcutConfig {
            val buttons = json.getJSONArray("buttons")
            require(buttons.length() in 1..3) { "电脑返回的快捷按钮数量无效" }
            val slots = List(buttons.length()) { index ->
                val button = buttons.getJSONObject(index)
                ShortcutButton(button.getString("id"), button.getString("name"), button.getString("mode")).also {
                    require(it.id in listOf("1", "2", "3") && it.name.isNotBlank() && it.mode in listOf("tap", "hold")) {
                        "电脑返回的快捷按钮无效"
                    }
                }
            }
            require(slots.map { it.id }.distinct().size == slots.size) { "电脑返回了重复的快捷按钮" }
            return ShortcutConfig(json.getLong("revision"), slots, json.optString("error"))
        }
    }
}

data class ServerStatus(
    val instance: String,
    val revision: Long,
    val modes: List<AudioMode>,
    val qualities: List<Quality>,
    val state: String,
    val error: String,
    val defaultMode: AudioMode,
    val defaultQuality: String,
    val directionError: String,
    val shortcuts: ShortcutConfig? = null,
) {
    companion object {
        fun fromJson(json: JSONObject): ServerStatus {
            val modes = json.getJSONArray("modes")
            val qualities = json.getJSONArray("qualities")
            return ServerStatus(
                json.getString("instance_id"), json.getLong("config_revision"),
                List(modes.length()) { AudioMode.parse(modes.getString(it)) },
                List(qualities.length()) { Quality.fromJson(qualities.getJSONObject(it)) },
                json.getString("state"), json.optString("error"),
                AudioMode.parse(json.getString("default_mode")),
                json.getJSONObject("default_quality").getString("id"),
                listOf("receive", "send").mapNotNull {
                    json.optJSONObject(it)?.optString("error")?.takeIf(String::isNotBlank)
                }.joinToString("；"),
                json.optJSONObject("shortcuts")?.let(ShortcutConfig::fromJson),
            )
        }
    }
}

data class ConnectionProfile(val origin: String, val pin: String, val token: String)
data class SessionOptions(val mode: AudioMode, val quality: String)

data class AudioState(
    val active: Boolean = false,
    val connected: Boolean = false,
    val origin: String = "",
    val message: String = "尚未连接",
    val muted: Boolean = false,
    val mode: AudioMode = AudioMode.MIC,
    val stats: String = "",
    val sessionId: String = "",
    val shortcuts: ShortcutConfig? = null,
)

fun normalizeOrigin(input: String): String {
    val url = input.trim().toHttpUrl()
    require(url.isHttps) { "请使用 https:// 开头的电脑地址" }
    require(url.username.isEmpty() && url.password.isEmpty()) { "地址不能包含用户名或密码" }
    require(url.encodedPath == "/" && url.query == null) { "请输入电脑根地址，例如 https://192.168.1.10:8443/" }
    return url.newBuilder().fragment(null).build().toString().removeSuffix("/")
}

class ApiException(val status: Int, val code: String, message: String) : Exception(message) {
    val terminal: Boolean
        get() = status in listOf(400, 401, 403, 409, 422) &&
            code !in listOf("configuration_changed", "session_replaced")
}
