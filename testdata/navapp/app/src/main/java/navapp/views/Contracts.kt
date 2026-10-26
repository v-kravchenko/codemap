package navapp.views

import android.content.Context
import android.content.Intent
import androidx.activity.result.contract.ActivityResultContract
import androidx.fragment.app.FragmentActivity

// An Activity opened through an ActivityResultContract: the framework calls createIntent on the
// contract a screen makes (SettingsFragment), so the screen opens it.
class PickContract : ActivityResultContract<Unit, String?>() {
    override fun createIntent(context: Context, input: Unit) = Intent(context, PickActivity::class.java)

    override fun parseResult(resultCode: Int, intent: Intent?) = intent?.getStringExtra("picked")
}

class PickActivity : FragmentActivity()
