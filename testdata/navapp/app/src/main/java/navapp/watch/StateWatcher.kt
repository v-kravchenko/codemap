package navapp.watch

import android.app.Activity

object StateWatcher {
    fun observe(from: Activity) {
        if (from.intent.getBooleanExtra("expired", false)) {
            SessionNavigator.signOut(from)
        }
    }
}
