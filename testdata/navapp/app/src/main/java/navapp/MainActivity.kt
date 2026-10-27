package navapp

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import navapp.nav3dsl.Nav3DslApp
import navapp.nav3when.Nav3WhenApp
import navapp.sections.SectionsApp
import navapp.strings.StringApp
import navapp.typed.TypedApp
import navapp.views.ViewsActivity

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            when (intent.getIntExtra("app", 0)) {
                0 -> TypedApp()
                1 -> StringApp()
                2 -> Nav3DslApp()
                3 -> Nav3WhenApp()
                4 -> SectionsApp()
                else -> startActivity(Intent(this, ViewsActivity::class.java))
            }
        }
    }
}
