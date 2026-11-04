package cool.cnb.rmic

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.disabled
import androidx.compose.ui.semantics.onClick
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.util.UUID

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun ShortcutPanel(state: ShortcutState, controller: ShortcutController) {
    val config = state.config ?: return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text("电脑快捷按钮", Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
                TextButton(onClick = controller::releaseAll, enabled = state.ready) { Text("全部释放") }
            }
            Text("${if (state.ready) "已连接" else "未就绪"} · ${state.message}",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            BoxWithConstraints(Modifier.fillMaxWidth()) {
                val columns = if (maxWidth >= 480.dp) config.buttons.size else if (maxWidth >= 320.dp && config.buttons.size == 2) 2 else 1
                val buttonWidth = (maxWidth - 8.dp * (columns - 1)) / columns
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    config.buttons.forEach { button ->
                        val enabled = state.ready && !state.busy && (button.mode == "hold" || state.heldButtons.isEmpty())
                        if (button.mode == "hold") {
                            HoldShortcutButton(button, enabled, button.id in state.heldButtons, config.revision, controller,
                                Modifier.width(buttonWidth))
                        } else {
                            OutlinedButton(onClick = { controller.run(button) }, enabled = enabled,
                                shape = RoundedCornerShape(12.dp), modifier = Modifier.width(buttonWidth).heightIn(min = 72.dp)) {
                                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                                    Text(button.name, style = MaterialTheme.typography.titleSmall)
                                    Text(if (state.busy) "执行中…" else "点击执行", style = MaterialTheme.typography.labelSmall)
                                }
                            }
                        }
                    }
                }
            }
            Text("按钮由电脑配置。按住时保持，松开即释放；切后台仅保留传音。",
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun HoldShortcutButton(
    button: ShortcutButton, enabled: Boolean, held: Boolean, revision: Long,
    controller: ShortcutController, modifier: Modifier = Modifier,
) {
    val gesture = remember(button.id, revision) { UUID.randomUUID().toString() }
    val scope = rememberCoroutineScope()
    DisposableEffect(gesture, enabled) { onDispose { controller.end(gesture); controller.end("$gesture-key"); controller.end("$gesture-accessibility") } }
    Surface(
        color = if (held) MaterialTheme.colorScheme.primaryContainer else MaterialTheme.colorScheme.surface,
        contentColor = when { !enabled -> MaterialTheme.colorScheme.onSurface.copy(alpha = .38f)
            held -> MaterialTheme.colorScheme.onPrimaryContainer
            else -> MaterialTheme.colorScheme.primary },
        shape = RoundedCornerShape(12.dp),
        border = BorderStroke(if (held) 2.dp else 1.dp, if (held) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outline),
        modifier = modifier.heightIn(min = 72.dp)
            .semantics(mergeDescendants = true) {
                role = Role.Button
                stateDescription = if (held) "保持中" else if (enabled) "松开" else "不可用"
                if (!enabled) disabled()
                onClick(label = "短暂按住并释放") {
                    if (!enabled) false else {
                        scope.launch {
                            val id = "$gesture-accessibility"
                            if (controller.begin(button, id)) try { delay(150) } finally { controller.end(id) }
                        }
                        true
                    }
                }
            }
            .onPreviewKeyEvent { event ->
                if (!enabled || event.key !in listOf(Key.Enter, Key.Spacebar, Key.DirectionCenter)) false
                else {
                    if (event.type == KeyEventType.KeyDown) controller.begin(button, "$gesture-key")
                    if (event.type == KeyEventType.KeyUp) controller.end("$gesture-key")
                    true
                }
            }
            .onFocusChanged { if (!it.isFocused) controller.end("$gesture-key") }
            .focusable(enabled)
            .pointerInput(gesture, enabled) {
                if (enabled) awaitEachGesture {
                    val down = awaitFirstDown()
                    if (controller.begin(button, gesture)) {
                        down.consume()
                        try { waitForUpOrCancellation() } finally { controller.end(gesture) }
                    }
                }
            },
    ) {
        Column(Modifier.padding(12.dp), horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center) {
            Text(button.name, style = MaterialTheme.typography.titleSmall)
            Text(if (held) "保持中 · 松开释放" else "按住保持", style = MaterialTheme.typography.labelSmall)
        }
    }
}
