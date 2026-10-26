package navapp.nav3when

import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.remember
import androidx.navigation3.runtime.NavEntry
import androidx.navigation3.ui.NavDisplay

// Navigation 3 with an entryProvider lambda: `is Key -> NavEntry(key)`.

data object Catalog

data class Item(val id: Int)

@Composable
fun Nav3WhenApp() {
    val backStack = remember { mutableStateListOf<Any>(Catalog) }
    NavDisplay(
        backStack = backStack,
        onBack = { backStack.removeLastOrNull() },
        entryProvider = { key ->
            when (key) {
                is Catalog ->
                    NavEntry(key) {
                        Button(onClick = { backStack.add(Item(3)) }) { Text("item") }
                    }
                is Item -> NavEntry(key) { Text("item ${key.id}") }
                else -> error("Unknown key: $key")
            }
        },
    )
}
