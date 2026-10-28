package navapp.sections

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import kotlinx.serialization.Serializable

@Serializable data object PhotoGuide

@Serializable data object DocsGuide

// An Activity registering its routes itself, one through an inline wrapper whose copy of the content
// is a class of the Activity: what the routes draw is theirs, so a route's preview goes with the
// route, not with the Activity too.
class GuideActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            val nav = rememberNavController()
            NavHost(nav, startDestination = if (intent.hasExtra("docs")) DocsGuide else PhotoGuide) {
                composable<PhotoGuide> { PhotoGuideScreen() }
                screen<DocsGuide> { DocsGuideScreen() }
            }
        }
    }
}
