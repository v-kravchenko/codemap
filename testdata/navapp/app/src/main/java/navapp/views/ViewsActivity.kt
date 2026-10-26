package navapp.views

import android.content.Intent
import android.os.Bundle
import android.view.View
import androidx.fragment.app.DialogFragment
import androidx.fragment.app.Fragment
import androidx.fragment.app.FragmentActivity
import androidx.navigation.fragment.findNavController
import navapp.R

// Fragments and an XML navigation graph: <action>, a nested graph, a dialog destination; an
// Activity
// opened with an Intent.

class ViewsActivity : FragmentActivity(R.layout.activity_views)

class ListFragment : Fragment(R.layout.fragment_list) {
    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        view.setOnClickListener { findNavController().navigate(R.id.action_list_to_detail) }
    }
}

class DetailFragment : Fragment(R.layout.fragment_list) {
    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        view.setOnClickListener { findNavController().navigate(R.id.settings_graph) }
        view.setOnLongClickListener {
            findNavController().navigate(R.id.infoDialog)
            true
        }
    }
}

class SettingsFragment : Fragment(R.layout.fragment_list) {
    private val pick = registerForActivityResult(PickContract()) {}

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        view.setOnClickListener { findNavController().navigate(R.id.action_settings_to_about) }
        view.setOnLongClickListener {
            pick.launch(Unit)
            true
        }
    }
}

class AboutFragment : Fragment(R.layout.fragment_list) {
    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        view.setOnClickListener {
            startActivity(Intent(requireContext(), LicensesActivity::class.java))
        }
    }
}

class InfoDialog : DialogFragment()

class LicensesActivity : FragmentActivity()

// A deep link into the same flow: a second way to every screen of ViewsActivity (nav-path --entry, --ways).
class LinkActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startActivity(Intent(this, ViewsActivity::class.java))
    }
}
