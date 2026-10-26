package navapp.nav3dsl

import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.navigation3.runtime.NavKey
import androidx.navigation3.runtime.entryProvider
import androidx.navigation3.runtime.rememberNavBackStack
import androidx.navigation3.ui.NavDisplay
import kotlinx.serialization.Serializable

// Navigation 3, the entryProvider DSL: the nav3 preset.

@Serializable data object Inbox : NavKey

@Serializable data class Message(val id: Int) : NavKey

@Serializable data object Compose : NavKey

@Composable
fun Nav3DslApp() {
    val backStack = rememberNavBackStack(Inbox)
    NavDisplay(
        backStack = backStack,
        onBack = { backStack.removeLastOrNull() },
        entryProvider =
            entryProvider {
                entry<Inbox> {
                    Button(onClick = { backStack.add(Message(7)) }) { Text("message") }
                }
                entry<Message> { key ->
                    Button(onClick = { backStack.add(Compose) }) { Text("reply to ${key.id}") }
                }
                entry<Compose> { Text("compose") }
            },
    )
}
