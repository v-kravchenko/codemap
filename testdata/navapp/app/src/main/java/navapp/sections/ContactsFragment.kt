package navapp.sections

import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.ComposeView
import androidx.fragment.app.Fragment

// A base whose fragments draw their content in a composable they override.
abstract class ComposeFragment : Fragment() {
    @Composable
    abstract fun SetContent()

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, savedInstanceState: Bundle?): View =
        ComposeView(inflater.context).apply { setContent { SetContent() } }
}

// A screen whose composables and their preview are members of its class: the preview goes with it.
class ContactsFragment : ComposeFragment() {
    @Composable
    override fun SetContent() {
        ContactsScreen("support@navapp")
    }

    @Composable
    private fun ContactsScreen(email: String) {
        ContactsRow(email)
    }

    @Composable
    private fun ContactsRow(email: String) {
        Text(email)
    }

    @Preview
    @Composable
    private fun ContactsScreenPreview() {
        ContactsScreen("preview@navapp")
    }
}
