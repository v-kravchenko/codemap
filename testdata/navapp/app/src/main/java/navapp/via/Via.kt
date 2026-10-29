package navapp.via

import android.os.Bundle
import androidx.fragment.app.FragmentActivity

// How a change reaches a screen (impact's via): MessageA reaches MessageActivity directly and
// InboxActivity only through MessageRegistry, a hub naming every Message; Flags.fast reaches
// TargetActivity directly and the screens opening others through Router (Router.kt) only as
// openers, EntryActivity too: it alone calls Router.restore, a method of the same navigator.

interface Message

class MessageA : Message

class MessageB : Message

class MessageC : Message

object MessageRegistry {
    fun decode(type: String): Message = when (type) {
        "a" -> MessageA()
        "b" -> MessageB()
        else -> MessageC()
    }
}

class InboxActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        title = MessageRegistry.decode(intent.type.orEmpty()).toString()
    }
}

class MessageActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        title = MessageA().toString()
    }
}

object Flags {
    var fast = false
}

class TargetActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        title = Flags.fast.toString()
    }
}

class SlowActivity : FragmentActivity()

class FirstOpenerActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Router.open(this)
    }
}

class SecondOpenerActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Router.open(this)
    }
}

class ThirdOpenerActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Router.open(this)
    }
}

class EntryActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Router.restore(this)
    }
}
