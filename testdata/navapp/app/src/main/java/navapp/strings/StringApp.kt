package navapp.strings

import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.navigation
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument

// Navigation Compose with string routes: the nav2-compose-strings preset.

const val FEED = "feed"

@Composable
fun StringApp() {
    val nav = rememberNavController()
    NavHost(nav, startDestination = FEED) {
        composable(FEED) {
            Button(onClick = { nav.navigate("post/42") }) { Text("post") }
            Button(onClick = { nav.navigate("account") }) { Text("account") }
        }
        composable(
            "post/{id}",
            arguments = listOf(navArgument("id") { type = NavType.StringType }),
        ) { entry ->
            val id = entry.arguments?.getString("id")
            Button(onClick = { nav.navigate("comments/$id?sort=new") }) { Text("comments") }
        }
        composable("comments/{postId}") { Text("comments") }
        navigation(startDestination = "signin", route = "account") {
            composable("signin") {
                Button(
                    onClick = { nav.navigate(FEED) { popUpTo("account") { inclusive = true } } }
                ) {
                    Text("in")
                }
            }
        }
    }
}
