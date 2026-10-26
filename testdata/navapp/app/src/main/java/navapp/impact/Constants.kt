package navapp.impact

import android.os.Bundle
import androidx.fragment.app.FragmentActivity

// Kotlin inlines a const val, so its users come from the sources: MAX_ITEMS affects LimitActivity.

const val MAX_ITEMS = 20

class LimitActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        title = "max $MAX_ITEMS"
    }
}
