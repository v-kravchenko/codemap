package navapp.sections

import androidx.compose.runtime.Composable

@Composable
fun PhotoGuideScreen() {
    GuideBody(rememberGuideTitle("photo"))
}

@Preview
@Composable
private fun PhotoGuidePreview() {
    GuideBody("photo")
}
