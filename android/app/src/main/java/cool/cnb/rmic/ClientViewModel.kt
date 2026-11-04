package cool.cnb.rmic

import android.app.Application
import android.os.Build
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import org.json.JSONObject

data class ClientUiState(
    val address: String = "",
    val servers: List<String> = emptyList(),
    val selectedOrigin: String = "",
    val name: String = "${Build.MANUFACTURER} ${Build.MODEL}",
    val remember: Boolean = true,
    val busy: Boolean = false,
    val authorized: Boolean = false,
    val trusted: Boolean = false,
    val pendingOrigin: String = "",
    val pendingPin: String = "",
    val pairCode: String = "",
    val message: String = "输入电脑 TUI 显示的 HTTPS 地址",
    val error: String = "",
    val status: ServerStatus? = null,
    val mode: AudioMode = AudioMode.MIC,
    val quality: String = "balanced",
)

class ClientViewModel(application: Application, private val store: ProfileStore) : AndroidViewModel(application) {
    constructor(application: Application) : this(application, ProfileStore(application))
    private val mutableUi = MutableStateFlow(ClientUiState())
    val ui = mutableUi.asStateFlow()
    val shortcutController = ShortcutController(viewModelScope)
    val shortcuts = shortcutController.state
    private var operation: Job? = null
    private var capabilityRefresh: Job? = null

    fun foreground(value: Boolean) {
        shortcutController.foreground(value)
        capabilityRefresh?.cancel()
        capabilityRefresh = if (value) viewModelScope.launch {
            while (true) {
                delay(3000)
                val profile = ClientRuntime.profile
                if (!ClientRuntime.audio.value.active && ui.value.authorized && !ui.value.busy && profile?.origin == ui.value.selectedOrigin) {
                    try {
                        val status = RmicApi(checkNotNull(profile)).use { it.status() }
                        if (ClientRuntime.profile == profile && !ClientRuntime.audio.value.active) {
                            mutableUi.update { it.copy(status = status) }
                        }
                    } catch (cancelled: CancellationException) { throw cancelled }
                    catch (error: ApiException) {
                        if (error.status == 401) mutableUi.update { it.copy(authorized = false, status = null, error = "授权已失效，请重新配对") }
                    } catch (_: Exception) { /* Keep audio controls available during temporary network loss. */ }
                }
            }
        } else null
    }

    init {
        perform {
            updateServers()
            val origin = ClientRuntime.profile?.origin ?: withContext(Dispatchers.IO) { store.selectedOrigin() }
            if (origin != null) openServer(origin)
        }
        viewModelScope.launch {
            ClientRuntime.audio.collect { audio ->
                val profile = ClientRuntime.profile
                if (audio.connected && audio.origin == ui.value.selectedOrigin) {
                    mutableUi.update { it.copy(status = it.status?.copy(shortcuts = audio.shortcuts)) }
                }
                updateShortcuts()
                if (!audio.active && profile?.origin == ui.value.selectedOrigin && profile.token.isEmpty()) {
                    mutableUi.update { it.copy(authorized = false, status = null) }
                }
            }
        }
        viewModelScope.launch { ui.collect { updateShortcuts() } }
    }

    private fun updateShortcuts() {
        val audio = ClientRuntime.audio.value
        val profile = ClientRuntime.profile?.takeIf { it.origin == ui.value.selectedOrigin && ui.value.authorized }
        val config = if (audio.active) audio.shortcuts else ui.value.status?.shortcuts
        val session = audio.sessionId.takeIf { audio.connected && audio.origin == profile?.origin }.orEmpty()
        shortcutController.update(profile, session, config)
    }

    override fun onCleared() { shortcutController.close(); super.onCleared() }
    fun address(value: String) {
        val matches = runCatching { normalizeOrigin(value) }.getOrNull() == ui.value.selectedOrigin
        mutableUi.update {
            if (matches) it.copy(address = value)
            else it.copy(address = value, trusted = false, authorized = false, status = null)
        }
    }
    fun name(value: String) { mutableUi.update { it.copy(name = value) } }
    fun remember(value: Boolean) { mutableUi.update { it.copy(remember = value) } }
    fun mode(value: AudioMode) {
        mutableUi.update { it.copy(mode = value, quality = if (it.quality == "ultra" && value != AudioMode.PLAYBACK) "music" else it.quality) }
    }
    fun quality(value: String) { mutableUi.update { it.copy(quality = value) } }
    fun showError(message: String) { mutableUi.update { it.copy(error = message) } }

    fun selectServer(origin: String) = perform {
        check(!ClientRuntime.audio.value.active) { "请先停止传音再切换服务器" }
        openServer(origin)
    }

    fun addServer() {
        if (ui.value.busy || ClientRuntime.audio.value.active) return
        ClientRuntime.profile = null
        ClientRuntime.mutableAudio.value = AudioState()
        mutableUi.update { ClientUiState(servers = it.servers, name = it.name, remember = it.remember,
            message = "输入另一台电脑的地址，各台电脑需要独立配对") }
    }

    fun inspect() = perform {
        check(!ClientRuntime.audio.value.active) { "请先停止传音" }
        val origin = normalizeOrigin(ui.value.address)
        if (origin in withContext(Dispatchers.IO) { store.origins() }) {
            openServer(origin)
        } else {
            val pin = ServerTrust.inspect(origin)
            mutableUi.update { it.copy(pendingOrigin = origin, pendingPin = pin) }
        }
    }

    fun confirmTrust() = perform {
        check(!ClientRuntime.audio.value.active) { "请先停止传音" }
        val state = ui.value
        check(state.pendingPin.isNotEmpty()) { "请先检查电脑证书" }
        val profile = ConnectionProfile(state.pendingOrigin, state.pendingPin, "")
        withContext(Dispatchers.IO) { store.save(profile, false) }
        updateServers()
        showProfile(profile)
    }

    fun cancelTrust() { mutableUi.update { it.copy(pendingOrigin = "", pendingPin = "") } }

    fun pair() = perform {
        check(!ClientRuntime.audio.value.active) { "请先停止传音" }
        val profile = checkNotNull(ClientRuntime.profile) { "请先检查并确认电脑证书" }
        check(normalizeOrigin(ui.value.address) == profile.origin) { "电脑地址已改变，请先检查连接" }
        val name = ui.value.name.trim()
        require(name.isNotEmpty()) { "请输入设备名称" }
        val remember = ui.value.remember
        RmicApi(profile.copy(token = "")).use { api ->
            val ticket = api.request("/api/pair/request", "POST", JSONObject().put("name", name).put("remember", remember))
            mutableUi.update { it.copy(pairCode = ticket.getString("code"), message = "请在电脑按 p，核对确认码并允许") }
            withTimeout(125_000) {
                while (true) {
                    delay(1000)
                    val result = api.request("/api/pair/status", "POST", JSONObject()
                        .put("id", ticket.getString("id")).put("secret", ticket.getString("secret")))
                    when (result.getString("state")) {
                        "pending" -> Unit
                        "approved" -> {
                            val token = result.getString("token")
                            require(token.matches(Regex("[a-f0-9]{48}"))) { "电脑返回了无效授权" }
                            val approved = profile.copy(token = token)
                            withContext(Dispatchers.IO) { store.save(approved, remember && result.optBoolean("remember")) }
                            ClientRuntime.profile = approved
                            refresh(approved)
                            break
                        }
                        else -> error("电脑拒绝了配对，请重新申请")
                    }
                }
            }
        }
    }

    fun forget() = perform {
        check(!ClientRuntime.audio.value.active) { "请先停止传音" }
        val profile = checkNotNull(ClientRuntime.profile) { "请先选择服务器，或仅清除其本机记录" }
        if (profile.token.isNotEmpty()) {
            try { RmicApi(profile).use { it.request("/api/pair/forget", "POST") } }
            catch (error: ApiException) { if (error.status != 401) throw error }
        }
        removeSelected("已忘记此服务器与授权，其他服务器记录保留")
    }

    fun clearLocal() = perform {
        check(!ClientRuntime.audio.value.active) { "请先停止传音" }
        removeSelected("已清除此服务器的本机记录；电脑上的授权可在 TUI 中撤销")
    }

    fun cancelOperation() { operation?.cancel() }

    fun options(): SessionOptions {
        val state = ui.value
        val profile = checkNotNull(ClientRuntime.profile) { "请先配对" }
        check(state.authorized && profile.token.isNotEmpty()) { "请先配对" }
        check(normalizeOrigin(state.address) == profile.origin && state.selectedOrigin == profile.origin) { "电脑地址已改变，请先检查连接" }
        check(state.mode in (state.status?.modes ?: emptyList())) { "请先检查电脑连接和可用模式" }
        return SessionOptions(state.mode, state.quality)
    }

    private suspend fun updateServers() {
        val origins = withContext(Dispatchers.IO) { store.origins() }
        mutableUi.update { it.copy(servers = origins) }
    }

    private suspend fun openServer(origin: String) {
        withContext(Dispatchers.IO) { store.select(origin) }
        mutableUi.update { it.copy(selectedOrigin = origin, address = origin, trusted = false,
            authorized = false, status = null, pendingOrigin = "", pendingPin = "") }
        if (ClientRuntime.profile?.origin != origin) ClientRuntime.profile = null
        val profile = ClientRuntime.cachedProfile(origin) ?: withContext(Dispatchers.IO) { store.load(origin) }
        showProfile(checkNotNull(profile) { "未保存此服务器" })
        if (profile.token.isNotEmpty()) refresh(profile)
    }

    private fun showProfile(profile: ConnectionProfile) {
        ClientRuntime.profile = profile
        if (!ClientRuntime.audio.value.active) ClientRuntime.mutableAudio.value = AudioState()
        mutableUi.update { it.copy(address = profile.origin, selectedOrigin = profile.origin,
            trusted = true, authorized = profile.token.isNotEmpty(), status = null,
            pendingOrigin = "", pendingPin = "", mode = AudioMode.MIC, quality = "balanced",
            message = if (profile.token.isEmpty()) "已信任此电脑，请申请配对" else "已保存此电脑的授权，正在检查连接") }
    }

    private suspend fun removeSelected(message: String) {
        val origin = ui.value.selectedOrigin
        require(origin.isNotEmpty()) { "请先选择要清除的服务器" }
        withContext(Dispatchers.IO) { store.remove(origin) }
        ClientRuntime.forgetProfile(origin)
        val origins = withContext(Dispatchers.IO) { store.origins() }
        mutableUi.update { ClientUiState(servers = origins, name = it.name, remember = it.remember, message = message) }
        ClientRuntime.mutableAudio.value = AudioState()
    }

    private suspend fun refresh(profile: ConnectionProfile) {
        val status = try {
            RmicApi(profile).use { it.status() }
        } catch (error: ApiException) {
            if (error.status == 401) {
                val cleared = profile.copy(token = "")
                withContext(Dispatchers.IO) { store.save(cleared, false, select = false) }
                ClientRuntime.updateProfile(cleared)
                if (ui.value.selectedOrigin == profile.origin) mutableUi.update { it.copy(authorized = false, status = null) }
            }
            throw error
        }
        mutableUi.update { it.copy(status = status, authorized = true, trusted = true,
            mode = status.defaultMode, quality = status.defaultQuality,
            message = "此电脑已许可，可以开始传音") }
    }

    private fun perform(block: suspend () -> Unit) {
        if (operation?.isCompleted == false) return
        operation = viewModelScope.launch {
            mutableUi.update { it.copy(busy = true, error = "") }
            try {
                block()
            } catch (timeout: kotlinx.coroutines.TimeoutCancellationException) {
                showError("配对等待超时，请重新申请")
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Exception) {
                showError(error.message ?: "操作失败")
                ClientRuntime.record("电脑操作失败：${error.javaClass.simpleName}")
            } finally {
                mutableUi.update { it.copy(busy = false, pairCode = "") }
            }
        }
    }
}
