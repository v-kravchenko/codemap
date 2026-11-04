package navapp.watch

import android.app.Activity

object FormBinder {
    fun bind(form: Activity) {
        form.title = form.javaClass.simpleName
        StateWatcher.observe(form)
    }
}
