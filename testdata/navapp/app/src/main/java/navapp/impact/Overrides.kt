package navapp.impact

import android.os.Bundle
import androidx.fragment.app.FragmentActivity

// An override cuts the walk: BaseActivity.observeLogout runs in onCreate of every subclass except
// QuietActivity, which overrides it.

abstract class BaseActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        observeLogout()
    }

    open fun observeLogout() {
        Session.listen()
    }
}

class LoudActivity : BaseActivity()

class QuietActivity : BaseActivity() {
    override fun observeLogout() = Unit
}

object Session {
    fun listen() {}
}
