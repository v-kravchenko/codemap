package navapp.impact

import android.os.Bundle
import androidx.fragment.app.FragmentActivity

// Generated code does not spread a change: Profile.nickname is read by NicknameActivity only;
// AvatarActivity uses copy() and equals(), which read every field.

data class Profile(val nickname: String, val avatar: String)

class NicknameActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        title = Profile("n", "a").nickname
    }
}

class AvatarActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val p = Profile("n", "a")
        title = (p.copy(avatar = "b") == p).toString() + p.avatar
    }
}
