package cool.cnb.rmic

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainActivity : ComponentActivity() {
    private val model: ClientViewModel by viewModels()
    private var resumed = false

    override fun onResume() { super.onResume(); resumed = true; model.foreground(hasWindowFocus()) }
    override fun onPause() { resumed = false; model.foreground(false); super.onPause() }
    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        model.foreground(resumed && hasFocus)
    }
    private val permissions = registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        launchAudio()
    }
    private val export = registerForActivityResult(ActivityResultContracts.CreateDocument("text/plain")) { uri ->
        if (uri != null) lifecycleScope.launch {
            try {
                val text = ClientRuntime.diagnostics()
                withContext(Dispatchers.IO) {
                    checkNotNull(contentResolver.openOutputStream(uri)).bufferedWriter().use { it.write(text) }
                }
            } catch (error: Exception) { model.showError(error.message ?: "无法导出诊断") }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge(statusBarStyle = SystemBarStyle.dark(android.graphics.Color.TRANSPARENT))
        setContent {
            MaterialTheme(colorScheme = darkColorScheme(primary = Color(0xFF6ADAC7), background = Color(0xFF10191D))) {
                val ui by model.ui.collectAsStateWithLifecycle()
                val audio by ClientRuntime.audio.collectAsStateWithLifecycle()
                val shortcuts by model.shortcuts.collectAsStateWithLifecycle()
                ClientScreen(ui, audio, shortcuts, model, ::requestStart,
                    { serviceAction(AudioService.ACTION_STOP) },
                    { serviceAction(AudioService.ACTION_MUTE) },
                    { export.launch("rmic-android-diagnostics.txt") },
                    { startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)) })
            }
        }
    }

    private fun requestStart() {
        try {
            val options = model.options()
            val required = mutableListOf<String>()
            if (options.mode.microphone && ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
                required += Manifest.permission.RECORD_AUDIO
            }
            if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                required += Manifest.permission.POST_NOTIFICATIONS
            }
            if (required.isEmpty()) launchAudio() else permissions.launch(required.toTypedArray())
        } catch (error: Exception) { model.showError(error.message ?: "无法开始") }
    }

    private fun launchAudio() {
        try {
            val options = model.options()
            if (options.mode.microphone && ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
                model.showError("麦克风权限未允许，请在系统应用设置中允许后重新开始")
                return
            }
            ContextCompat.startForegroundService(this, Intent(this, AudioService::class.java)
                .setAction(AudioService.ACTION_START).putExtra("origin", checkNotNull(ClientRuntime.profile).origin).putExtra("mode", options.mode.wire).putExtra("quality", options.quality))
        } catch (error: Exception) { model.showError(error.message ?: "无法启动后台音频") }
    }

    private fun serviceAction(action: String) {
        if (ClientRuntime.audio.value.active) startService(Intent(this, AudioService::class.java).setAction(action))
    }
}

@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
private fun ClientScreen(
    ui: ClientUiState, audio: AudioState, shortcuts: ShortcutState, model: ClientViewModel,
    onStart: () -> Unit, onStop: () -> Unit, onMute: () -> Unit,
    onExport: () -> Unit, onBatterySettings: () -> Unit,
) {
    val editable = !ui.busy && !audio.active
    var computerExpanded by rememberSaveable(ui.selectedOrigin) { mutableStateOf(false) }
    var toolsExpanded by rememberSaveable { mutableStateOf(false) }
    Scaffold { padding ->
        Column(Modifier.fillMaxSize().padding(padding).imePadding().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("RMic", style = MaterialTheme.typography.headlineLarge, color = MaterialTheme.colorScheme.primary)
            if (ui.error.isNotBlank()) SelectionContainer {
                Text(ui.error, color = MaterialTheme.colorScheme.error)
            }
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Column(Modifier.weight(1f)) {
                            Text("电脑与配对", style = MaterialTheme.typography.titleMedium)
                            if (ui.authorized) Text(ui.selectedOrigin, style = MaterialTheme.typography.bodySmall)
                        }
                        if (ui.authorized && !ui.busy) TextButton(onClick = { computerExpanded = !computerExpanded }) {
                            Text(if (computerExpanded) "收起" else "管理")
                        }
                    }
                    if (computerExpanded || !ui.authorized || ui.busy) {
                        if (ui.servers.isNotEmpty()) {
                            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                                ui.servers.forEach { origin ->
                                    FilterChip(selected = ui.selectedOrigin == origin,
                                        onClick = { model.selectServer(origin) }, enabled = editable,
                                        label = { Text(origin) }, modifier = Modifier.fillMaxWidth())
                                }
                            }
                            OutlinedButton(onClick = model::addServer, enabled = editable) { Text("添加电脑") }
                        }
                        OutlinedTextField(value = ui.address, onValueChange = model::address,
                            label = { Text("电脑 HTTPS 地址") }, placeholder = { Text("https://192.168.1.10:8443/") },
                            singleLine = true, enabled = editable, modifier = Modifier.fillMaxWidth(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri))
                        if (!ui.authorized || ui.busy) Text(ui.message, style = MaterialTheme.typography.bodyMedium)
                        if (ui.busy) {
                            CircularProgressIndicator()
                            if (ui.pairCode.isNotEmpty()) Text(ui.pairCode, style = MaterialTheme.typography.displaySmall)
                            TextButton(onClick = model::cancelOperation) { Text("取消等待") }
                        } else {
                            OutlinedButton(onClick = { model.inspect() }, enabled = editable) { Text("检查连接") }
                        }
                        if (!ui.authorized) {
                            OutlinedTextField(value = ui.name, onValueChange = model::name,
                                label = { Text("设备名称") }, singleLine = true, enabled = editable,
                                modifier = Modifier.fillMaxWidth())
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Checkbox(checked = ui.remember, onCheckedChange = model::remember, enabled = editable)
                                Text("记住此设备")
                            }
                            Button(onClick = { model.pair() }, enabled = editable && ui.trusted) { Text("申请配对") }
                        }
                    }
                }
            }
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(audio.message, style = MaterialTheme.typography.titleLarge)
                    if (audio.active) {
                        Text(audio.origin, style = MaterialTheme.typography.bodySmall)
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            Button(onClick = onStop) { Text("停止传音") }
                            if (audio.mode.microphone) OutlinedButton(onClick = onMute) {
                                Text(if (audio.muted) "取消静音" else "麦克风静音")
                            }
                        }
                    } else {
                        Text("传音模式", style = MaterialTheme.typography.titleMedium)
                        AudioMode.entries.forEach { mode ->
                            FilterChip(selected = ui.mode == mode, onClick = { model.mode(mode) },
                                label = { Text(mode.label) }, modifier = Modifier.fillMaxWidth(),
                                enabled = editable && mode in (ui.status?.modes ?: emptyList()))
                        }
                        if (ui.mode == AudioMode.DUPLEX) Text("建议戴耳机，减少回声。", style = MaterialTheme.typography.bodySmall)
                        if (ui.status != null) {
                            Text("音质", style = MaterialTheme.typography.titleSmall)
                            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                ui.status.qualities.filter { !it.playbackOnly || ui.mode == AudioMode.PLAYBACK }.forEach { quality ->
                                    FilterChip(selected = ui.quality == quality.id, onClick = { model.quality(quality.id) },
                                        enabled = editable, label = { Text(qualityLabel(quality.id)) })
                                }
                            }
                        }
                        Button(onClick = onStart, modifier = Modifier.fillMaxWidth(),
                            enabled = editable && ui.authorized && ui.status != null) { Text("开始传音") }
                    }
                }
            }
            if (shortcuts.config != null) {
                ShortcutPanel(shortcuts, model.shortcutController)
            }
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    TextButton(onClick = { toolsExpanded = !toolsExpanded }, modifier = Modifier.fillMaxWidth()) {
                        Text(if (toolsExpanded) "收起更多设置" else "更多设置", Modifier.weight(1f))
                        Text(if (toolsExpanded) "−" else "+")
                    }
                    if (toolsExpanded) {
                        if (audio.stats.isNotBlank()) Text(audio.stats, style = MaterialTheme.typography.bodySmall)
                        ui.status?.qualities?.firstOrNull { it.id == ui.quality }?.let {
                            Text("Opus · ${it.bitrate / 1000} kbps / 方向 · ${it.channels} 声道", style = MaterialTheme.typography.bodySmall)
                        }
                        TextButton(onClick = onBatterySettings) { Text("后台电池设置") }
                        TextButton(onClick = onExport) { Text("导出诊断") }
                        if (ui.selectedOrigin.isNotEmpty()) {
                            Text(ui.selectedOrigin, style = MaterialTheme.typography.bodySmall)
                            OutlinedButton(onClick = { model.forget() }, enabled = editable) { Text("撤销授权并忘记电脑") }
                            TextButton(onClick = { model.clearLocal() }, enabled = editable) { Text("仅清除本机记录") }
                        }
                    }
                }
            }
            Spacer(Modifier.height(8.dp))
        }
    }
    if (ui.pendingPin.isNotEmpty()) {
        AlertDialog(onDismissRequest = model::cancelTrust,
            title = { Text("确认电脑身份") },
            text = {
                Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(ui.pendingOrigin)
                    Text("请核对电脑上的证书公钥指纹，确认一致后再信任。")
                    SelectionContainer { Text(ui.pendingPin, style = MaterialTheme.typography.bodySmall) }
                }
            },
            confirmButton = { TextButton(onClick = { model.confirmTrust() }, enabled = !ui.busy) { Text("已核对，信任") } },
            dismissButton = { TextButton(onClick = model::cancelTrust) { Text("取消") } })
    }
}

private fun qualityLabel(id: String) = when (id) {
    "voice" -> "语音"
    "balanced" -> "均衡"
    "music" -> "高音质"
    "weak" -> "弱网络"
    "ultra" -> "Ultra"
    else -> id
}
