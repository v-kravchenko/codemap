package navapp.impact

import androidx.fragment.app.FragmentActivity

// A constructor overrides nothing: a change to FirstActivity's constructor stays in FirstActivity,
// not in SecondActivity with the same base.

abstract class CounterActivity : FragmentActivity() {
    var count = 0
}

class FirstActivity : CounterActivity() {
    init {
        count = 1
    }
}

class SecondActivity : CounterActivity() {
    init {
        count = 2
    }
}
