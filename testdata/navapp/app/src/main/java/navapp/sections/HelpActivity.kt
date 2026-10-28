package navapp.sections

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.Composable

// An Activity drawing the body the guides share through a screen of another file; its file's
// preview of that body goes with it alone, not with the guides too.
class HelpActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent { HelpScreen() }
    }
}

@Preview
@Composable
private fun HelpPreview() {
    GuideBody("help")
}
