package navapp.via

import android.content.Context
import android.content.Intent

// A navigator in a file of its own: open is the hub the opener screens share, restore is called by
// EntryActivity alone.
object Router {
    fun next(): Class<*> = if (Flags.fast) TargetActivity::class.java else SlowActivity::class.java

    fun open(context: Context) {
        context.startActivity(Intent(context, next()))
    }

    fun restore(context: Context) {
        context.startActivity(Intent(context, next()).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP))
    }
}
