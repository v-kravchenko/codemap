package navapp.sections

import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.navigation
import androidx.navigation.compose.rememberNavController
import kotlinx.serialization.Serializable

// A project's own DSL over Navigation Compose: inline wrappers copied into each registration, a
// section of the graph in a function of its own, and content without captures (ComposableSingletons).

@Serializable data object Onboarding

@Serializable data object Welcome

@Serializable data object Terms

@Serializable data object About

inline fun <reified T : Any> NavGraphBuilder.screen(crossinline content: @Composable () -> Unit) {
    composable<T> { Frame { content() } }
}

inline fun <reified T : Any> NavGraphBuilder.section(
    start: Any,
    crossinline builder: NavGraphBuilder.() -> Unit,
) {
    navigation<T>(startDestination = start) { builder() }
}

@Composable
fun SectionsApp() {
    val nav = rememberNavController()
    NavHost(nav, startDestination = Onboarding) {
        section<Onboarding>(Welcome) {
            onboardingSteps(onNext = { nav.navigate(Terms) }, onAbout = { nav.navigate(About) })
        }
        composable<About> { AboutScreen() }
    }
}

fun NavGraphBuilder.onboardingSteps(onNext: () -> Unit, onAbout: () -> Unit) {
    screen<Welcome> { WelcomeScreen(onNext) }
    screen<Terms> { TermsScreen(onAbout) }
}

@Composable
fun Frame(content: @Composable () -> Unit) {
    content()
}

@Composable
fun WelcomeScreen(onNext: () -> Unit) {
    Button(onClick = onNext) { Text("next") }
}

@Composable
fun TermsScreen(onAbout: () -> Unit) {
    Button(onClick = onAbout) { Text("about") }
}

@Composable
fun AboutScreen() {
    Text("about")
}

// Previews: each goes with the screen whose own composable it draws, not with every screen of the
// file; SectionsTheme, which no screen draws, picks none.
annotation class Preview

@Composable
fun SectionsTheme(content: @Composable () -> Unit) {
    content()
}

@Preview
@Composable
fun WelcomeScreenPreview() {
    SectionsTheme { WelcomeScreen(onNext = {}) }
}

@Preview
@Composable
fun TermsScreenPreview() {
    TermsScreen(onAbout = {})
}
