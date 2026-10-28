package navapp.sections

import androidx.compose.runtime.Composable

// A route's preview in a file of its own: it goes with the route, not with GuideActivity, whose
// NavHost registers it.
@Preview
@Composable
private fun DocsGuidePreview() {
    DocsGuideScreen()
}
