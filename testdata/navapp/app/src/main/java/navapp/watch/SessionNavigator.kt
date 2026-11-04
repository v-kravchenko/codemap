package navapp.watch

import android.app.Activity
import android.content.Intent
import androidx.fragment.app.FragmentActivity

// Ways through code many screens share, one navigator calling another: ten forms bind through
// FormBinder, two screens call StateWatcher themselves, and StateWatcher signs out through
// SessionNavigator. The lowest node with ten screens below is FormBinder alone; every way of the two
// passes StateWatcher, which twelve screens share: their navigator too.

object SessionNavigator {
    fun signOut(from: Activity) = from.startActivity(Intent(from, SignOutActivity::class.java))
}

class SignOutActivity : FragmentActivity()
