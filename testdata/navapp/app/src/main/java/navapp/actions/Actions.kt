package navapp.actions

import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable

// What calls a screen's ViewModel: its screen through a function reference handed to a private
// composable, its own constructor, the base class's code for a hook the subclass overrides.

@Serializable data object Contacts

@Composable
fun ContactsScreen(vm: ContactsViewModel) {
    ContactsHeader(vm)
    ContactsContent(onSearch = vm::search)
}

@Composable
private fun ContactsHeader(vm: ContactsViewModel) {
    Button(onClick = { vm.search("") }) { Text("all") }
}

@Composable
private fun ContactsContent(onSearch: (String) -> Unit) {
    Button(onClick = { onSearch("a") }) { Text("search") }
}

class ContactsViewModel : ViewModel() {
    val items = MutableStateFlow(emptyList<String>())

    init {
        viewModelScope.launch { items.value = getContacts() }
    }

    fun search(query: String) {
        items.value = getContacts().filter { it.contains(query) }
    }

    fun getContacts(): List<String> = validators()

    private fun validators(): List<String> = listOf("a")
}

abstract class BaseSignViewModel : ViewModel() {
    protected abstract fun getDeepLink(): String

    open fun openNextScreen() {}

    fun onContinueClick() {
        viewModelScope.launch {
            println(getDeepLink())
            openNextScreen()
        }
    }
}

class SignViewModel : BaseSignViewModel() {
    override fun getDeepLink() = "app://sign"

    override fun openNextScreen() {
        println("next")
    }
}
