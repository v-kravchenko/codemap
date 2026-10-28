package navapp.sections

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember

// A body two screens of other files draw (PhotoGuide, DocsGuide): a preview of it in one of those
// files goes with that file's screen, not with both.
@Composable
fun GuideBody(title: String) {
    Text(rememberGuideTitle(title))
}

// A composable returning a value draws nothing: not in a screen's or a preview's content.
@Composable
fun rememberGuideTitle(title: String): String = remember(title) { "guide: $title" }
