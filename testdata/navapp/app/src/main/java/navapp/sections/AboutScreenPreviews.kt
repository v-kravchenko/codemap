package navapp.sections

import androidx.compose.runtime.Composable

// AboutPreview moved here from AboutPreviews.kt; ScreenPreview is one of two the base's map lacks
// (TermsPreviews has the base's one), so it is not a move (changes_test.go).
@Preview
@Composable
private fun AboutPreview() {
    AboutScreen()
}

@Preview
@Composable
private fun ScreenPreview() {
    TermsScreen(onAbout = {})
}
