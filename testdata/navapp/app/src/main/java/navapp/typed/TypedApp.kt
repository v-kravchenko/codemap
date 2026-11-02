package navapp.typed

import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.dialog
import androidx.navigation.compose.navigation
import androidx.navigation.compose.rememberNavController
import androidx.navigation.toRoute
import navapp.actions.Contacts
import navapp.actions.ContactsScreen
import navapp.actions.ContactsViewModel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable

// Navigation Compose with type-safe routes: the nav2-compose preset.

@Serializable data object Home

@Serializable data class Profile(val id: String)

@Serializable data object Settings

@Serializable data object Confirm

@Serializable data object AuthGraph

@Serializable data object Login

@Composable
fun TypedApp() {
    val nav = rememberNavController()
    NavHost(nav, startDestination = Home) {
        composable<Home> {
            HomeScreen(
                onProfile = { nav.navigate(Profile("1")) },
                onSettings = { nav.navigate(Settings) },
                onAuth = { nav.navigate(AuthGraph) },
                onContacts = { nav.navigate(Contacts) },
            )
        }
        composable<Profile> { entry ->
            ProfileScreen(entry.toRoute<Profile>().id, onSettings = { nav.navigate(Settings) })
        }
        composable<Settings> { Text("settings") }
        composable<Contacts> { ContactsScreen(ContactsViewModel()) }
        dialog<Confirm> { Text("confirm") }
        authSection(onConfirm = { nav.navigate(Confirm) })
    }
}

fun androidx.navigation.NavGraphBuilder.authSection(onConfirm: () -> Unit) {
    navigation<AuthGraph>(startDestination = Login) {
        composable<Login> { LoginScreen(onConfirm) }
    }
}

@Composable
fun HomeScreen(onProfile: () -> Unit, onSettings: () -> Unit, onAuth: () -> Unit, onContacts: () -> Unit) {
    Button(onClick = onProfile) { Text("profile") }
    Button(onClick = onSettings) { Text("settings") }
    Button(onClick = onAuth) { Text("auth") }
    Button(onClick = onContacts) { Text("contacts") }
}

@Composable
fun ProfileScreen(id: String, onSettings: () -> Unit) {
    Button(onClick = onSettings) { Text(id) }
}

@Composable
fun LoginScreen(onConfirm: () -> Unit) {
    Button(onClick = onConfirm) { Text("login") }
}

class SettingsViewModel : ViewModel() {
    val events = MutableSharedFlow<Any>()

    fun openSettings() {
        viewModelScope.launch { events.emit(Settings) }
    }
}
