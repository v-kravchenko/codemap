package navapp.impact

import android.app.Activity
import android.os.Bundle
import androidx.fragment.app.FragmentActivity

// A default argument reaches only the callers that leave it out: defaultLabel() affects
// PlainCopyActivity, not LabelledCopyActivity.

fun Activity.copyText(text: String, label: String = defaultLabel()) {
    title = label + text
}

fun defaultLabel(): String = "copied"

class PlainCopyActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        copyText("a")
    }
}

class LabelledCopyActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        copyText("b", "label")
    }
}
